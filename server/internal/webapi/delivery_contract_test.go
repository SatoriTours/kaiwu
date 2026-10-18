package webapi

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestDeliveryPolicyReadOverSocketAndPreflightWireContract(t *testing.T) {
	fake := &fakeDelivery{replay: map[string]bool{}}
	store, srv, client, _ := deliveryAPI(t, fake)
	receipt, err := store.Command(workbench.Command{OperationID: "project-wire", Type: "add_project", Revision: store.Snapshot().Revision, Name: "project", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	h := New(Config{Host: u.Host, Token: strings.Repeat("x", 32), Delivery: fake}, store, nil).(*handler)
	defer h.Close()
	req := httptest.NewRequest("GET", srv.URL+"/api/socket", nil)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Cookie", cookieOf(client))
	reply := h.socketCall(req, socketRequest{ID: "read", Method: "delivery/policy?projectId=" + url.QueryEscape(receipt.CreatedID)})
	if reply.Status != 200 {
		t.Fatalf("policy read: %d %s", reply.Status, reply.Data)
	}
	var policy struct {
		Policy workbench.ProjectDeliveryPolicy `json:"policy"`
	}
	if err = json.Unmarshal(reply.Data, &policy); err != nil || policy.Policy.ProjectID != receipt.CreatedID {
		t.Fatalf("wrong policy: %s %v", reply.Data, err)
	}
	b, _ := json.Marshal(map[string]string{"projectId": receipt.CreatedID})
	reply = h.socketCall(req, socketRequest{ID: "preflight", Method: "delivery/preflight", Body: b})
	var result map[string]any
	_ = json.Unmarshal(reply.Data, &result)
	if reply.Status != 200 || result["ready"] != false || result["blockers"] == nil {
		t.Fatalf("preflight must expose lowerCamelCase contract: %d %s", reply.Status, reply.Data)
	}
	for _, method := range []string{"delivery/policy?projectId=" + receipt.CreatedID + "&path=/etc/passwd", "delivery/policy?projectId=a&projectId=b"} {
		if out := h.socketCall(req, socketRequest{ID: "bad", Method: method}); out.Status != 400 {
			t.Fatalf("invalid query accepted: %s %d", method, out.Status)
		}
	}
}
