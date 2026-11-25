package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestThinkingOverrideIsExplicitAndOptional(t *testing.T) {
	for _, disable := range []bool{false, true} {
		f := newFixture(t)
		f.a.c.LLMDisableThinking = disable
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request map[string]json.RawMessage
			if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
				t.Error(e)
			}
			value, exists := request["chat_template_kwargs"]
			if exists != disable {
				t.Errorf("override presence=%v, disable=%v", exists, disable)
			}
			if exists {
				var kwargs map[string]bool
				if e := json.Unmarshal(value, &kwargs); e != nil || len(kwargs) != 1 || kwargs["enable_thinking"] {
					t.Errorf("invalid thinking override %s", value)
				}
			}
			write(w, 200, object{"choices": []object{{"message": object{"content": `{"action":"skip"}`}}}})
		}))
		f.a.c.LLMURL = server.URL
		if _, e := f.a.chatContent(context.Background(), "system", object{}); e != nil {
			t.Fatal(e)
		}
		server.Close()
	}
}

func TestEmptyToolCallsAreNotAnOperation(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		allow bool
	}{
		{`null`, true}, {`[]`, true}, {`[ {"type":"function","function":{"name":"change_settings"}} ]`, false}, {`{}`, false}, {`"invalid"`, false},
	} {
		f := newFixture(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"skip\"}","tool_calls":` + tc.raw + `}}]}`))
		}))
		f.a.c.LLMURL = server.URL
		_, e := f.a.chatContent(context.Background(), "system", object{})
		if (e == nil) != tc.allow {
			t.Errorf("tool_calls=%s allowed=%v err=%v", tc.raw, tc.allow, e)
		}
		server.Close()
	}
}
