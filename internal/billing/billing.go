// Package billing defines subscription tariffs and the rules for turning a
// chosen tariff into a subscription length. Prices are in rubles; durations
// are whole months. It has no I/O — it is the pure pricing model the payment
// API and the admin tooling share.
package billing

import (
	"errors"
	"fmt"
	"time"
)

// daysPerMonth is the subscription month length used to convert a tariff's
// months into a concrete expiry. 30 days keeps pricing predictable.
const daysPerMonth = 30

// ErrUnknownTariff is returned when a tariff ID does not match any catalog entry.
var ErrUnknownTariff = errors.New("unknown tariff")

// Tariff is one purchasable subscription option.
type Tariff struct {
	// ID is the stable identifier the client sends back when signing up.
	ID string `json:"id"`
	// Months is the subscription length in months.
	Months int `json:"months"`
	// PriceRUB is the price in whole rubles.
	PriceRUB int `json:"priceRub"`
	// Title is a human-readable label for the UI.
	Title string `json:"title"`
}

// TTL returns the subscription duration this tariff grants.
func (t Tariff) TTL() time.Duration {
	return time.Duration(t.Months*daysPerMonth) * 24 * time.Hour
}

// Catalog returns the available tariffs in display order. These are the prices
// offered to new clients: 1/2/3/6 months.
func Catalog() []Tariff {
	return []Tariff{
		{ID: "1m", Months: 1, PriceRUB: 400, Title: "1 месяц"},
		{ID: "2m", Months: 2, PriceRUB: 780, Title: "2 месяца"},
		{ID: "3m", Months: 3, PriceRUB: 1100, Title: "3 месяца"},
		{ID: "6m", Months: 6, PriceRUB: 2200, Title: "6 месяцев"},
	}
}

// Lookup returns the tariff with the given ID, or ErrUnknownTariff.
func Lookup(id string) (Tariff, error) {
	for _, t := range Catalog() {
		if t.ID == id {
			return t, nil
		}
	}
	return Tariff{}, fmt.Errorf("%w: %q", ErrUnknownTariff, id)
}
