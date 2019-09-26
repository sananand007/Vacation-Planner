package solution

import (
	"Vacation-planner/POI"
	"Vacation-planner/graph"
	"Vacation-planner/iowrappers"
	"Vacation-planner/matching"
	log "github.com/sirupsen/logrus"
	"strconv"
	"strings"
	"time"
)

const CandidateQueueLength = 20
const CandidateQueueDisplay = 15

type TripEvent struct {
	tag        uint8
	startTime  time.Time
	endTime    time.Time
	startPlace matching.Place
	endPlace   matching.Place
}

// Find top solution candidates
func FindBestCandidates(candidates []SlotSolutionCandidate) []SlotSolutionCandidate {
	m := make(map[string]SlotSolutionCandidate) // map for result extraction
	vertexes := make([]graph.Vertex, len(candidates))
	for idx, candidate := range candidates {
		candidateKey := strconv.FormatInt(int64(idx), 10)
		vertex := graph.Vertex{Name: candidateKey, Key: candidate.Score}
		vertexes[idx] = vertex
		m[candidateKey] = candidate
	}
	// use limited-size minimum priority queue
	priorityQueue := graph.MinPriorityQueue{Nodes: make([]graph.Vertex, 0)}
	for _, vertex := range vertexes {
		if priorityQueue.Size() == CandidateQueueLength {
			top := priorityQueue.GetRoot()
			if vertex.Key > top.Key {
				priorityQueue.ExtractTop()
			} else {
				continue
			}
		}
		priorityQueue.Insert(vertex)
	}

	// remove extra vertexes from priority queue
	for priorityQueue.Size() > CandidateQueueDisplay {
		priorityQueue.ExtractTop()
	}

	res := make([]SlotSolutionCandidate, 0)

	for priorityQueue.Size() > 0 {
		res = append(res, m[priorityQueue.ExtractTop()])
	}

	return res
}

// Generate slot solution candidates
// Parameter list matches slot request
func GenerateSlotSolution(timeMatcher *matching.TimeMatcher, location string, evTag string, stayTimes []matching.TimeSlot,
	radius uint, weekday POI.Weekday, redisClient iowrappers.RedisClient) (slotSolution SlotSolution) {
	if len(stayTimes) != len(evTag) {
		log.Fatal("User designated stay time does not match tag.")
		return
	}

	intervals := make([]POI.TimeInterval, len(stayTimes))
	for idx, stayTime := range stayTimes {
		intervals[idx] = stayTime.Slot
	}

	cityCountry := strings.Split(location, ",")
	evTags := make([]string, len(stayTimes))
	for idx, c := range evTag {
		evTags[idx] = string(c)
	}

	redisReq := iowrappers.SlotSolutionCacheRequest{
		Country:   cityCountry[1],
		City:      cityCountry[0],
		Radius:    uint64(radius),
		EVTags:    evTags,
		Intervals: intervals,
		Weekday:   weekday,
	}

	slotSolutionCacheResp, err := redisClient.GetSlotSolution(redisReq)
	if err == nil { // cache hit
		for _, candidate := range slotSolutionCacheResp.SlotSolutionCandidate {
			slotSolutionCandidate := SlotSolutionCandidate{
				PlaceNames:      candidate.PlaceNames,
				PlaceIDS:        candidate.PlaceIds,
				PlaceLocations:  candidate.PlaceLocations,
				EndPlaceDefault: matching.Place{},
				Score:           candidate.Score,
				IsSet:           true,
			}
			slotSolution.SlotSolutionCandidates = append(slotSolution.SlotSolutionCandidates, slotSolutionCandidate)
		}
		return
	}

	slotSolution.SetTag(evTag)
	if !slotSolution.IsSlotTagValid() {
		log.Fatalf("Slot tag %s is invalid.", evTag)
		return
	}

	slotSolution.SlotSolutionCandidates = make([]SlotSolutionCandidate, 0)
	slotCandidates := make([]SlotSolutionCandidate, 0)

	req := matching.TimeMatchingRequest{}

	req.Location = location
	if radius <= 0 {
		radius = 2000
	}
	req.Radius = radius

	queryTimeSlot := matching.TimeSlot{
		Slot: POI.TimeInterval{
			Start: stayTimes[0].Slot.Start,
			End:   stayTimes[len(stayTimes)-1].Slot.End,
		},
	}
	// only one big time slot
	req.TimeSlots = []matching.TimeSlot{queryTimeSlot}

	if weekday < POI.DATE_MONDAY || weekday > POI.DATE_SUNDAY {
		weekday = POI.DATE_SATURDAY
	}
	req.Weekday = weekday

	placeClusters := timeMatcher.Matching(&req)

	categorizedPlaces := Categorize(&placeClusters[0])
	minuteLimit := GetSlotLengthinMin(&placeClusters[0])

	mdIter := MDtagIter{}
	mdIter.Init(evTag, categorizedPlaces)

	for mdIter.HasNext() {
		curCandidate := slotSolution.CreateCandidate(mdIter, categorizedPlaces)

		if curCandidate.IsSet {
			_, travelTimeInMin := GetTravelTimeByDistance(categorizedPlaces, mdIter)
			if travelTimeInMin <= float64(minuteLimit) {
				//FIXME: ADD TRIP EVENT GENERATION FUNCTION CALL
				slotCandidates = append(slotCandidates, curCandidate)
			}
		}
		mdIter.Next()
	}
	bestCandidates := FindBestCandidates(slotCandidates)
	slotSolution.SlotSolutionCandidates = append(slotSolution.SlotSolutionCandidates, bestCandidates...)

	// cache slot solution calculation results
	slotSolutionToCache := iowrappers.SlotSolutionCacheResponse{}
	slotSolutionToCache.SlotSolutionCandidate = make([]iowrappers.SlotSolutionCandidateCache, len(slotSolution.SlotSolutionCandidates))

	for idx, slotSolutionCandidate := range slotSolution.SlotSolutionCandidates {
		candidateCache := iowrappers.SlotSolutionCandidateCache{
			PlaceIds:       slotSolutionCandidate.PlaceIDS,
			Score:          slotSolutionCandidate.Score,
			PlaceNames:     slotSolutionCandidate.PlaceNames,
			PlaceLocations: slotSolutionCandidate.PlaceLocations,
		}
		slotSolutionToCache.SlotSolutionCandidate[idx] = candidateCache
	}

	redisClient.CacheSlotSolution(redisReq, slotSolutionToCache)

	return
}