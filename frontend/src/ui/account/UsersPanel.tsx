import { useState } from "preact/hooks";
import type { User, UserRole } from "../../models/user";
import { Check, Loader, Users, X } from "../primitives/icons";
import { EmptyState, ErrorBanner } from "../primitives/Feedback";

interface UsersPanelProps {
  currentEmail: string;
  isAdmin: boolean;
  users: User[] | null;
  loading: boolean;
  error: string | null;
  canAddUsers: boolean;
  onAdd: (email: string, role: UserRole) => Promise<void>;
  onRemove: (email: string) => Promise<void>;
  onSetRole: (email: string, role: UserRole) => Promise<void>;
}

// UsersPanel is the admin-only directory for sign-in eligibility. Mirrors
// the SecretsBody pattern: collapsible card, add-form on top, row actions
// on the right. Renders a friendly notice (instead of hard-failing) when
// the caller isn't an admin so non-admins who reach the account page still
// see something sensible.
export function UsersPanel({
  currentEmail,
  isAdmin,
  users,
  loading,
  error,
  canAddUsers,
  onAdd,
  onRemove,
  onSetRole,
}: UsersPanelProps) {
  const removeUser = async (email: string) => {
    if (!confirm(`Remove ${email}? They lose access immediately.`)) return;
    await onRemove(email);
  };

  if (!isAdmin) {
    return (
      <section class="rounded-lg border border-white/10 bg-[#101318] p-4 text-[13px] text-ink-300">
        Users are admin-only. Ask your admin to add or remove people.
      </section>
    );
  }

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Users</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
            Anyone listed here can sign in. Admins manage users and delete
            projects; members can be added to specific projects.
          </div>
        </div>
        {loading && <Loader class="w-4 h-4 mt-2 text-ink-300 animate-spin" />}
      </header>

      <div class="p-3 space-y-3">
        {error && <ErrorBanner message={error} />}
        {canAddUsers ? (
          <AddUserForm onAdd={onAdd} />
        ) : (
          <div class="rounded-md border border-accent-yellow/25 bg-accent-yellow/[0.08] px-3 py-2.5 text-[12.5px] text-accent-yellow">
            Configure Google sign-in above before adding users.
          </div>
        )}
        <UserList
          users={users ?? []}
          loading={loading && users == null}
          currentEmail={currentEmail}
          onRemove={removeUser}
          onSetRole={onSetRole}
        />
      </div>
    </section>
  );
}

function AddUserForm({
  onAdd,
}: {
  onAdd: (email: string, role: UserRole) => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<UserRole>("member");
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async (e: Event) => {
    e.preventDefault();
    const em = email.trim().toLowerCase();
    if (!em) {
      setErr("Email is required.");
      return;
    }
    if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(em)) {
      setErr("That doesn't look like an email.");
      return;
    }
    setErr(null);
    setSubmitting(true);
    try {
      await onAdd(em, role);
      setEmail("");
      setRole("member");
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={submit}
      class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 space-y-2"
    >
      <div class="grid gap-2 sm:grid-cols-[2fr_auto_auto] items-center">
        <input
          type="email"
          value={email}
          onInput={(e) => setEmail((e.target as HTMLInputElement).value)}
          placeholder="someone@example.com"
          spellcheck={false}
          autoComplete="off"
          class="h-9 px-2.5 rounded-md border border-white/10 bg-white/[0.04] text-[13px] text-ink-50 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue/60 focus:ring-1 focus:ring-accent-blue/30"
        />
        <select
          value={role}
          onChange={(e) => setRole((e.target as HTMLSelectElement).value as UserRole)}
          class="h-9 px-2 rounded-md border border-white/10 bg-white/[0.04] text-[13px] text-ink-50 focus:outline-none focus:border-accent-blue/60 focus:ring-1 focus:ring-accent-blue/30"
        >
          <option value="member">member</option>
          <option value="admin">admin</option>
        </select>
        <button
          type="submit"
          disabled={submitting}
          class="h-9 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {submitting ? "Adding…" : "Add"}
        </button>
      </div>
      {err && <div class="text-[11.5px] text-accent-red">{err}</div>}
    </form>
  );
}

function UserList({
  users,
  loading,
  currentEmail,
  onRemove,
  onSetRole,
}: {
  users: User[];
  loading: boolean;
  currentEmail: string;
  onRemove: (email: string) => Promise<void>;
  onSetRole: (email: string, role: UserRole) => Promise<void>;
}) {
  if (loading) {
    return (
      <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-4 text-center text-[12.5px] text-ink-300">
        Loading users…
      </div>
    );
  }
  if (users.length === 0) {
    return (
      <EmptyState Icon={Users} title="No users yet" hint="Add an email above to let someone sign in." />
    );
  }
  return (
    <div class="space-y-2">
      {users.map((u) => (
        <UserRow
          key={u.email}
          user={u}
          isSelf={u.email === currentEmail.toLowerCase()}
          onRemove={() => onRemove(u.email)}
          onSetRole={(r) => onSetRole(u.email, r)}
        />
      ))}
    </div>
  );
}

function UserRow({
  user,
  isSelf,
  onRemove,
  onSetRole,
}: {
  user: User;
  isSelf: boolean;
  onRemove: () => Promise<void>;
  onSetRole: (role: UserRole) => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const toggleRole = async () => {
    const next: UserRole = user.role === "admin" ? "member" : "admin";
    setBusy(true);
    setErr(null);
    try {
      await onSetRole(next);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    setErr(null);
    try {
      await onRemove();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2 space-y-1">
      <div class="flex items-center gap-2 min-w-0">
        <span class="text-[12.5px] text-ink-50 truncate" title={user.email}>
          {user.email}
        </span>
        <span
          class={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium ${
            user.role === "admin"
              ? "text-accent-blue bg-accent-blue/[0.14]"
              : "text-ink-300 bg-white/[0.06]"
          }`}
        >
          {user.role}
        </span>
        {isSelf && (
          <span class="inline-flex items-center h-5 px-1.5 rounded text-[11px] text-accent-green bg-accent-green/[0.10]">
            <Check class="w-3 h-3 mr-1" /> you
          </span>
        )}
        <div class="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={toggleRole}
            disabled={busy}
            class="h-7 px-2 rounded text-[11px] text-ink-300 hover:text-ink-100 hover:bg-white/[0.08] disabled:opacity-50"
            title={user.role === "admin" ? "Demote to member" : "Promote to admin"}
          >
            {user.role === "admin" ? "demote" : "promote"}
          </button>
          <button
            type="button"
            onClick={remove}
            disabled={busy}
            class="h-7 w-7 rounded text-ink-300 hover:text-accent-red hover:bg-white/[0.08] grid place-items-center disabled:opacity-50"
            aria-label={`Remove ${user.email}`}
            title="Remove user"
          >
            <X class="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
      {err && <div class="text-[11.5px] text-accent-red">{err}</div>}
    </div>
  );
}
