package knapsack

import (
	"github.com/stretchr/testify/assert"
	"github.com/weihesdlegend/Vacation-planner/POI"
	"github.com/weihesdlegend/Vacation-planner/matching"
	"github.com/weihesdlegend/Vacation-planner/utils"
	"testing"
)

func TestKnapsack(t *testing.T) {
	var priceAllZero bool
	priceAllZero = false
	places := make([]matching.Place, 20, 20)
	err := utils.ReadFromFile("data/test_visit_random_gen.json", &places)
	if err != nil || len(places) == 0 {
		t.Fatal("Json file read error")
	}
	t.Logf("number of places from the input is %d", len(places))
	for _, p := range places {
		if p.Price != 0.0 {
			priceAllZero = true
		}
	}
	if !priceAllZero {
		t.Fatal("all the prices are zero.")
	}
	timeLimit := uint8(8)
	budget := uint(80)
	querystart := matching.QueryTimeStart{StartHour:8, Day:POI.DateMonday, EndHour:24}
	result := matching.KnapsackV1(places, querystart, timeLimit, budget)
	if len(result) == 0 {
		t.Error("No result is returned.")
	}
	result2, totalCost, totalTimeSpent := matching.Knapsack(places, querystart, timeLimit, budget)
	t.Logf("total cost of the trip is %d", totalCost)
	t.Logf("total time of the trip is %d", totalTimeSpent)

	assert.LessOrEqual(t, totalTimeSpent, timeLimit, "")
	assert.LessOrEqual(t, totalCost, budget, "")

	if len(result) == 0 {
		t.Error("No result is returned by v2")
	}
	for _, p := range result {
		t.Logf("Placename: %s, ID: %s", p.Name, p.PlaceId)
	}
	t.Logf("Knapsack V1 result size: %d", len(result))
	for _, p := range result2 {
		t.Logf("Placename: %s, ID: %s", p.Name, p.PlaceId)
	}
	t.Logf("Knapsack V2 result size: %d", len(result2))
	if len(result) != len(result2) {
		t.Error("v2 result doesn't match")
	}
	for i := range result {
		if result[i].PlaceId != result2[i].PlaceId {
			t.Error("v2 result is not the same")
		}
	}
	assert.Equal(t,"ChIJkwQn2FnxNIgRXbZ_Wu4cdL0", result2[0].PlaceId, "Assert result[0] is expected")
	assert.Equal(t,"ChIJXzT5vqv2NIgRfqEmldPesjc", result2[2].PlaceId, "Assert result[2] is expected")
}
