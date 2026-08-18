// Package sshprobe answers one question about a stored SSH target: can this
// key reach that host right now?
//
// The probe runs from the host, never from a container, because an operator
// needs the answer while adding a target — before any project has been
// synced. The private key is written to a temp file with mode 0600 for the
// duration of one command and deleted immediately afterwards, and neither the
// key nor its path ever reaches the returned output.
package sshprobe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

const (
	// connectTimeout is passed to ssh itself so a black-holed port fails in
	// seconds rather than at the TCP default.
	connectTimeout = 8
	// probeTimeout bounds the whole command, including a server that accepts
	// the connection and then stalls.
	probeTimeout = 20 * time.Second
	// maxOutputBytes keeps a chatty MOTD from filling an API response.
	maxOutputBytes = 2000
	// redaction replaces the temp key path anywhere it appears in output.
	redaction = "<key>"
)

// ErrUnavailable reports that no ssh client is installed on this host.
var ErrUnavailable = errors.New("ssh client is not available on this host")

// Prober runs the OpenSSH client.
type Prober struct{}

func New() *Prober { return &Prober{} }

// Probe opens one non-interactive session and runs `echo ok`. A refused or
// unauthenticated connection is a normal answer (OK=false), not an error;
// only a host that cannot run the probe at all returns one.
func (p *Prober) Probe(
	ctx context.Context,
	target serviceglobalsecrets.SSHTarget,
) (serviceglobalsecrets.TestResult, error) {
	if strings.TrimSpace(target.PrivateKey) == "" {
		return serviceglobalsecrets.TestResult{}, serviceglobalsecrets.ErrNoValue
	}
	binary, err := exec.LookPath("ssh")
	if err != nil {
		return serviceglobalsecrets.TestResult{}, ErrUnavailable
	}

	keyPath, cleanupKey, err := writeTempFile("futrx-probe-key-*", ensureTrailingNewline(target.PrivateKey))
	if err != nil {
		return serviceglobalsecrets.TestResult{}, err
	}
	defer cleanupKey()

	knownHostsPath := ""
	if line := strings.TrimSpace(target.KnownHostsLine); line != "" {
		path, cleanupKnown, err := writeTempFile("futrx-probe-known-*", line+"\n")
		if err != nil {
			return serviceglobalsecrets.TestResult{}, err
		}
		defer cleanupKnown()
		knownHostsPath = path
	}

	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	command := exec.CommandContext(pctx, binary, probeArgs(target, keyPath, knownHostsPath)...)
	// A prompt would hang the probe forever; BatchMode covers ssh itself and
	// this covers anything ssh would otherwise delegate to an askpass helper.
	command.Env = append(os.Environ(), "SSH_ASKPASS_REQUIRE=never", "DISPLAY=")
	started := time.Now()
	output, runErr := command.CombinedOutput()
	latency := time.Since(started).Milliseconds()

	text := redact(string(output), keyPath, knownHostsPath)
	if runErr != nil && text == "" {
		text = redact(runErr.Error(), keyPath, knownHostsPath)
	}
	return serviceglobalsecrets.TestResult{
		OK:        runErr == nil,
		Output:    text,
		LatencyMS: latency,
	}, nil
}

// probeArgs builds the equivalent of `ssh <name> 'echo ok'` without relying
// on a config entry: the host has none, only the containers do.
func probeArgs(target serviceglobalsecrets.SSHTarget, keyPath, knownHostsPath string) []string {
	strictness := "accept-new"
	knownHosts := os.DevNull
	if knownHostsPath != "" {
		strictness = "yes"
		knownHosts = knownHostsPath
	}
	return []string{
		"-i", keyPath,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + strconv.Itoa(connectTimeout),
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=" + strictness,
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "LogLevel=ERROR",
		"-p", strconv.Itoa(target.EffectivePort()),
		target.User + "@" + target.Host,
		"echo ok",
	}
}

func writeTempFile(pattern, content string) (string, func(), error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, errors.New("could not stage the probe key")
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	// OpenSSH refuses a key file other users can read, so the mode is set
	// before the key is written rather than after.
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, errors.New("could not secure the probe key")
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, errors.New("could not stage the probe key")
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, errors.New("could not stage the probe key")
	}
	return file.Name(), cleanup, nil
}

// redact removes the temp paths from anything the caller will see and caps
// the length. The key content itself is never echoed by ssh, but the paths
// are, and a path in an error invites someone to go looking for the file.
func redact(text string, paths ...string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		text = strings.ReplaceAll(text, path, redaction)
	}
	text = strings.TrimSpace(text)
	if len(text) > maxOutputBytes {
		text = text[:maxOutputBytes] + "…"
	}
	return text
}

func ensureTrailingNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}
