package reconcile

import (
	"testing"
	"time"
)

func TestLastTouchedAt(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	fallback := now.Add(-2 * time.Hour)

	touched, ok := lastTouchedAt(map[string]string{annotationLastUpdatedAt: now.Format(time.RFC3339)}, fallback)
	if !ok {
		t.Fatalf("expected ok")
	}
	if !touched.Equal(now) {
		t.Fatalf("expected %v, got %v", now, touched)
	}

	created := now.Add(-24 * time.Hour)
	touched, ok = lastTouchedAt(map[string]string{annotationCreatedAt: created.Format(time.RFC3339)}, fallback)
	if !ok {
		t.Fatalf("expected ok")
	}
	if !touched.Equal(created) {
		t.Fatalf("expected %v, got %v", created, touched)
	}

	touched, ok = lastTouchedAt(nil, fallback)
	if !ok {
		t.Fatalf("expected ok")
	}
	if !touched.Equal(fallback) {
		t.Fatalf("expected %v, got %v", fallback, touched)
	}
}

func TestPreviewIdentityFromLabels(t *testing.T) {
	labels := map[string]string{
		labelPreviewEnabled: "true",
		labelPRNumber:       "123",
		labelRepoFullName:   "hci-gu/auto-deployer",
	}

	ref, ok := previewIdentityFromLabels("previews", labels)
	if !ok {
		t.Fatalf("expected ok")
	}
	if ref.prNumber != 123 {
		t.Fatalf("expected pr=123, got %d", ref.prNumber)
	}
	if ref.repo != "hci-gu/auto-deployer" {
		t.Fatalf("unexpected repo %q", ref.repo)
	}
	if ref.namespace != "previews" {
		t.Fatalf("unexpected namespace %q", ref.namespace)
	}
}
