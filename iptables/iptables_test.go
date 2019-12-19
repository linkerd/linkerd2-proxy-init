package iptables

import (
	"reflect"
	"testing"
)

func TestMakeMultiportDestinations(t *testing.T) {
	assertEqual(t, makeMultiportDestinations([]string{}), [][]string{})
	assertEqual(t, makeMultiportDestinations([]string{"22", "25-27", "33"}), [][]string{{"22", "25:27", "33"}})
	assertEqual(t, makeMultiportDestinations([]string{"22-22", "25-27", "33"}), [][]string{{"22", "25:27", "33"}})
	assertEqual(t, makeMultiportDestinations([]string{"22", "25-27", "not-a-number", "33"}), [][]string{{"22", "25:27", "33"}})
	assertEqual(t, makeMultiportDestinations([]string{"22", "25-27", "notanumber", "33"}), [][]string{{"22", "25:27", "33"}})
}

func TestMakeMultiportDestinations_Split(t *testing.T) {
	assertEqual(t,
		makeMultiportDestinations([]string{"22-23", "25-27", "33-34", "35-35", "37-38", "50-54", "56-57", "60-63"}),
		[][]string{{"22:23", "25:27", "33:34", "35", "37:38", "50:54", "56:57", "60:63"}})
	assertEqual(t,
		makeMultiportDestinations([]string{"22-23", "25-27", "33-34", "35-35", "37-38", "50-54", "56", "58", "60", "63", "70-72"}),
		[][]string{{"22:23", "25:27", "33:34", "35", "37:38", "50:54", "56", "58", "60", "63"}, {"70:72"}})
}

func assertEqual(t *testing.T, check [][]string, expected [][]string) {
	if !reflect.DeepEqual(check, expected) {
		t.Fatalf("mismatch: got \"%s\" expected \"%s\"", check, expected)
	}
}
