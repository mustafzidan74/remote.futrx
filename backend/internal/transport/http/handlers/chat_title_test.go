package httphandlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// titleGeneratorStub stands in for the auxiliary model's chat-title driver.
type titleGeneratorStub struct {
	meta  servicechat.Meta
	err   error
	calls int
}

func (s *titleGeneratorStub) RegenerateTitle(
	context.Context,
	servicechat.ID,
) (servicechat.Meta, error) {
	s.calls++
	return s.meta, s.err
}

func postChatTitle(t *testing.T, handler *ChatHandler, method string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/chats/abcd1234/title", nil)
	recorder := httptest.NewRecorder()
	handler.HandleResource(recorder, request)
	return recorder
}

func TestChatTitleRouteIsAbsentWithoutAnAuxiliaryModel(t *testing.T) {
	// A deployment with no auxiliary model has no such action. 404 is the
	// honest answer, and it is what makes the browser hide the button.
	handler, _ := newPolicyChatHandler(t)

	recorder := postChatTitle(t, handler, http.MethodPost)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestChatTitleRegenerates(t *testing.T) {
	handler, _ := newPolicyChatHandler(t)
	titles := &titleGeneratorStub{
		meta: servicechat.Meta{ID: "abcd1234", Title: "Ship the checkout flow"},
	}
	handler.WithTitleGenerator(titles)

	recorder := postChatTitle(t, handler, http.MethodPost)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if titles.calls != 1 {
		t.Fatalf("the generator was called %d times, want once", titles.calls)
	}
	if !strings.Contains(recorder.Body.String(), "Ship the checkout flow") {
		t.Fatalf("body = %s, want the updated chat", recorder.Body.String())
	}
}

func TestChatTitleRejectsTheWrongMethod(t *testing.T) {
	handler, _ := newPolicyChatHandler(t)
	titles := &titleGeneratorStub{}
	handler.WithTitleGenerator(titles)

	recorder := postChatTitle(t, handler, http.MethodGet)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if titles.calls != 0 {
		t.Fatal("a GET reached the model")
	}
}

func TestChatTitleMapsModelFailuresToStatusesAnOperatorCanRead(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "the model is switched off",
			err:  serviceauxmodel.ErrDisabled,
			want: http.StatusServiceUnavailable,
		},
		{
			name: "the breaker is open after repeated failures",
			err:  serviceauxmodel.ErrBreakerOpen,
			want: http.StatusServiceUnavailable,
		},
		{
			name: "the chat has nothing to be named after",
			err:  fmt.Errorf("%w: this chat has no message yet", serviceauxmodel.ErrEmptyInput),
			want: http.StatusBadRequest,
		},
		{
			name: "the endpoint misbehaved",
			err:  errors.New("the endpoint responded 500"),
			want: http.StatusBadGateway,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _ := newPolicyChatHandler(t)
			handler.WithTitleGenerator(&titleGeneratorStub{err: test.err})

			recorder := postChatTitle(t, handler, http.MethodPost)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}
