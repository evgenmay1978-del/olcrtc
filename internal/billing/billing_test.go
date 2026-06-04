package billing

import (
	"errors"
	"testing"
	"time"
)

func TestCatalogMatchesPriceList(t *testing.T) {
	want := map[string]struct {
		months int
		price  int
	}{
		"1m": {1, 400},
		"2m": {2, 780},
		"3m": {3, 1100},
		"6m": {6, 2200},
	}
	cat := Catalog()
	if len(cat) != len(want) {
		t.Fatalf("catalog len = %d, want %d", len(cat), len(want))
	}
	for _, tr := range cat {
		w, ok := want[tr.ID]
		if !ok {
			t.Fatalf("unexpected tariff %q", tr.ID)
		}
		if tr.Months != w.months || tr.PriceRUB != w.price {
			t.Fatalf("tariff %q = %dm/%d₽, want %dm/%d₽", tr.ID, tr.Months, tr.PriceRUB, w.months, w.price)
		}
	}
}

func TestLookupAndTTL(t *testing.T) {
	tr, err := Lookup("3m")
	if err != nil {
		t.Fatalf("Lookup(3m): %v", err)
	}
	if want := 90 * 24 * time.Hour; tr.TTL() != want {
		t.Fatalf("3m TTL = %v, want %v", tr.TTL(), want)
	}
	if _, err := Lookup("nope"); !errors.Is(err, ErrUnknownTariff) {
		t.Fatalf("Lookup(nope) = %v, want ErrUnknownTariff", err)
	}
}
