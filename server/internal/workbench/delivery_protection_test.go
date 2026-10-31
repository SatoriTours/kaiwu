package workbench

import "testing"

func TestDeliveryProtectedDefinitionsCannotBeRebased(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d, err := s.Delivery(r.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	d.ProtectedFingerprint = "workflows-v1:original"
	d.Phase = "developing"
	d, err = s.AdvanceDelivery(d.ID, d.Version, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"", "workflows-v1:weaker"} {
		next := d
		next.ProtectedFingerprint = replacement
		if _, err := s.AdvanceDelivery(d.ID, d.Version, next, nil); err == nil {
			t.Fatal("frozen definition baseline was cleared/replaced within the same cycle")
		}
	}
}
