package service

import (
	"context"
	"strings"
	"testing"
)

// TestLoadCustomFixture_BannerSurvives verifies the end-to-end load path:
// fixture file → ReadCustomValues → newFlatValues → DocHeadComment populated
// on the FlatValues that the column wires to its CustomValues field.
func TestLoadCustomFixture_BannerSurvives(t *testing.T) {
	t.Parallel()

	svc := NewValuesService()

	domainVF, err := svc.ReadCustomValues(context.Background(), "../test-data/redis-values-cornercases.yaml")
	if err != nil {
		t.Skipf("fixture not present or unreadable: %v", err)
	}

	if domainVF.DocHeadComment == "" {
		t.Fatal("ReadCustomValues left DocHeadComment empty — banner extraction broken")
	}

	if !strings.Contains(domainVF.DocHeadComment, "Bitnami Redis") {
		t.Fatalf("DocHeadComment missing expected text:\n%s", domainVF.DocHeadComment)
	}

	flat := newFlatValues(domainVF)

	if flat.DocHeadComment == "" {
		t.Fatal("newFlatValues dropped DocHeadComment — column will see empty banner")
	}

	if !strings.Contains(flat.DocHeadComment, "Bitnami Redis") {
		t.Fatalf("FlatValues.DocHeadComment missing expected text:\n%s", flat.DocHeadComment)
	}
}
