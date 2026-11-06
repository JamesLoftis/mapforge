package ddprovider

import (
	"context"
	"strings"
	"testing"

	bridge "mapforge/bridgeclient"
)

func fetchAgainst(t *testing.T, reply, id string) (bool, string) {
	t.Helper()
	host, port := startReplyServer(t, reply)
	c := bridge.New(host, port)
	c.Attempts = 1
	_, exists, diags := fetchNode(context.Background(), c, id)
	var msgs []string
	for _, d := range diags.Errors() {
		msgs = append(msgs, d.Summary()+": "+d.Detail())
	}
	return exists, strings.Join(msgs, "; ")
}

func TestFetchNodeOfflineIsAnError(t *testing.T) {
	exists, errs := fetchAgainst(t,
		`{"ok":false,"error":"dungeondraft offline: bridge agent not connected","code":"offline"}`, "7")
	if exists || errs == "" {
		t.Fatalf("exists=%v errors=%q; want an error that leaves state alone", exists, errs)
	}
}

func TestFetchNodeNotFoundIsGone(t *testing.T) {
	exists, errs := fetchAgainst(t, `{"ok":false,"error":"no element with id 7","code":"not_found"}`, "7")
	if exists || errs != "" {
		t.Fatalf("exists=%v errors=%q; want gone with no error", exists, errs)
	}
}

func TestFetchNodeBadIDIsAnError(t *testing.T) {
	exists, errs := fetchAgainst(t, `{"ok":true,"result":{}}`, "abc")
	if exists || !strings.Contains(errs, "abc") {
		t.Fatalf("exists=%v errors=%q; want an error naming abc", exists, errs)
	}
}
