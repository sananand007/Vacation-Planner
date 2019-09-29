package iowrappers

import (
	"Vacation-planner/POI"
	"Vacation-planner/utils"
	"context"
	"errors"
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"googlemaps.github.io/maps"
	"strings"
	"sync"
	"time"
)

type LocationType string

const (
	LocationTypeCafe          = LocationType("cafe")
	LocationTypeRestaurant    = LocationType("restaurant")
	LocationTypeMuseum        = LocationType("museum")
	LocationTypeGallery       = LocationType("art_gallery")
	LocationTypeAmusementPark = LocationType("amusement_park")
	LocationTypePark          = LocationType("park")
)

const (
	GoogleNearbySearchDelay = time.Second
)

var detailedSearchFields = flag.String("fields", "name,opening_hours,formatted_address,adr_address", "a list of comma-separated fields")

// request generated by clustering layer
type PlaceSearchRequest struct {
	// "city,country"
	Location string
	// "visit", "eatery",...
	PlaceCat POI.PlaceCategory
	// search radius
	Radius uint
	// rank by
	RankBy string
	// maximum number of results, set this upper limit for reducing upper-layer computational load and limiting external API call
	MaxNumResults uint
	// minimum number of results, set this lower limit for reducing risk of zero result in upper-layer computations
	MinNumResults uint
}

func GoogleMapsNearbySearchWrapper(c MapsClient, location string, placeType string, radius uint,
	pageToken string, rankBy string) (resp maps.PlacesSearchResponse, err error) {
	latlng, err := maps.ParseLatLng(location)
	// since we have Redis and database before calling nearby search,
	// if location cannot be parsed, then the request cannot be fulfilled.
	if logErr(err, utils.LogError) {
		return
	}

	mapsReq := maps.NearbySearchRequest{
		Type:      maps.PlaceType(placeType),
		Location:  &latlng,
		Radius:    radius,
		PageToken: pageToken,
		RankBy:    maps.RankBy(rankBy),
	}
	resp, err = c.client.NearbySearch(context.Background(), &mapsReq)
	logErr(err, utils.LogError)
	return
}

func (c *MapsClient) NearbySearch(request *PlaceSearchRequest) (places []POI.Place, e error) {
	var maxReqTimes uint = 5
	places, e = c.ExtensiveNearbySearch(maxReqTimes, request)
	return
}

// ExtensiveNearbySearch tries to find a specified number of search results from a place category once for each location type in the category
// maxRequestTime specifies the number of times to query for each location type having maxRequestTimes provides Google API call protection
func (c *MapsClient) ExtensiveNearbySearch(maxRequestTimes uint, request *PlaceSearchRequest) (places []POI.Place, err error) {
	if request.RankBy == "" {
		request.RankBy = "prominence" // default rankBy value
	}

	placeTypes := getPlaceTypes(request.PlaceCat) // get place types in a category

	nextPageTokenMap := make(map[LocationType]string) // map for place type to search token
	for _, placeType := range placeTypes {
		nextPageTokenMap[placeType] = ""
	}

	var reqTimes uint = 0    // number of queries for each location type
	var totalResult uint = 0 // number of results so far, keep this number low

	microAddrMap := make(map[string]string) // map place ID to its micro-address
	placeMap := make(map[string]bool)       // remove duplication for place with same ID

	searchStartTime := time.Now()

	for totalResult < request.MinNumResults {
		// if error, return regardless of number of results obtained
		if err != nil {
			return
		}
		for _, placeType := range placeTypes {
			if reqTimes > 0 && nextPageTokenMap[placeType] == "" { // no more result for this location type
				continue
			}

			nextPageToken := nextPageTokenMap[placeType]
			searchResp, err := GoogleMapsNearbySearchWrapper(*c, request.Location, string(placeType), request.Radius, nextPageToken, request.RankBy)
      if err != nil {
        return 
      }
			placeIdMap := make(map[int]string) // maps index in search response to place ID
			for k, res := range searchResp.Results {
				if res.OpeningHours == nil || res.OpeningHours.WeekdayText == nil {
					placeIdMap[k] = res.PlaceID
				}
			}

			detailSearchResults := make([]PlaceDetailSearchRes, len(placeIdMap))
			var wg sync.WaitGroup
			wg.Add(len(placeIdMap))
			for idx, placeId := range placeIdMap {
				go c.DetailedSearchWrapper(idx, placeId, &detailSearchResults[idx], &wg)
			}
			wg.Wait()

			for _, placeDetails := range detailSearchResults {
				searchRespIdx := placeDetails.RespIdx
				searchResp.Results[searchRespIdx].OpeningHours = placeDetails.Res.OpeningHours
				searchResp.Results[searchRespIdx].FormattedAddress = placeDetails.Res.FormattedAddress
				microAddrMap[searchResp.Results[searchRespIdx].PlaceID] = placeDetails.Res.AdrAddress
			}

			places = append(places, parsePlacesSearchResponse(searchResp, placeType, microAddrMap, placeMap)...)
			totalResult += uint(len(searchResp.Results))
			nextPageTokenMap[placeType] = searchResp.NextPageToken
		}
		reqTimes++
		if reqTimes == maxRequestTimes {
			break
		}
		time.Sleep(GoogleNearbySearchDelay) // sleep to make sure new next page token comes to effect
	}

	searchDuration := time.Since(searchStartTime)

	// logging
	c.logger.WithFields(log.Fields{
		"center location (lat,lng)": request.Location,
		"place category":            request.PlaceCat,
		"total results":             totalResult,
		"Maps API call time":        searchDuration,
	}).Info("Logging nearby search")

	return
}

type PlaceDetailSearchRes struct {
	Res     *maps.PlaceDetailsResult
	RespIdx int
}

func (c *MapsClient) DetailedSearchWrapper(idx int, placeId string, detailSearchRes *PlaceDetailSearchRes, wg *sync.WaitGroup) {
	defer wg.Done()
	searchRes, err := c.PlaceDetailedSearch(placeId)
	if err != nil {
		log.Error(err)
		return
	}
	*detailSearchRes = PlaceDetailSearchRes{Res: &searchRes, RespIdx: idx}
	return
}

func (c *MapsClient) PlaceDetailedSearch(placeId string) (maps.PlaceDetailsResult, error) {
	if c.client == nil {
		return maps.PlaceDetailsResult{}, errors.New("client does not exist")
	}
	flag.Parse() // parse detailed search fields

	req := &maps.PlaceDetailsRequest{
		PlaceID: placeId,
	}

	if *detailedSearchFields != "" {
		fieldMask, err := parseFields(*detailedSearchFields)
		utils.CheckErrImmediate(err, utils.LogError)
		req.Fields = fieldMask
	}

	startSearchTime := time.Now()

	resp, err := c.client.PlaceDetails(context.Background(), req)
	utils.CheckErrImmediate(err, utils.LogError)

	searchDuration := time.Since(startSearchTime)

	// logging
	c.logger.WithFields(log.Fields{
		"place name":              resp.Name,
		"place formatted address": resp.FormattedAddress,
		"Maps API call time":      searchDuration,
	}).Info("Logging detailed place search")

	return resp, nil
}

func parsePlacesSearchResponse(resp maps.PlacesSearchResponse, locationType LocationType, microAddrMap map[string]string, placeMap map[string]bool) (places []POI.Place) {
	for _, res := range resp.Results {
		id := res.PlaceID
		if seen, _ := placeMap[id]; seen {
			continue
		} else {
			placeMap[id] = true
		}
		name := res.Name
		lat := fmt.Sprintf("%f", res.Geometry.Location.Lat)
		lng := fmt.Sprintf("%f", res.Geometry.Location.Lng)
		location := strings.Join([]string{lat, lng}, ",")
		addr := ""
		if microAddrMap != nil {
			addr = microAddrMap[res.ID]
		}
		priceLevel := res.PriceLevel
		h := &POI.OpeningHours{}
		if res.OpeningHours != nil && res.OpeningHours.WeekdayText != nil && len(res.OpeningHours.WeekdayText) > 0 {
			h.Hours = append(h.Hours, res.OpeningHours.WeekdayText...)
		}
		rating := res.Rating
		places = append(places, POI.CreatePlace(name, location, addr, res.FormattedAddress, string(locationType), h, id, priceLevel, rating))
	}
	return
}

// Given a location type returns a set of types defined in google maps API
func getPlaceTypes(placeCat POI.PlaceCategory) (placeTypes []LocationType) {
	switch placeCat {
	case POI.PlaceCategoryVisit:
		placeTypes = append(placeTypes,
			[]LocationType{LocationTypePark, LocationTypeAmusementPark, LocationTypeGallery, LocationTypeMuseum}...)
	case POI.PlaceCategoryEatery:
		placeTypes = append(placeTypes,
			[]LocationType{LocationTypeCafe, LocationTypeRestaurant}...)
	}
	return
}

func getPlaceCategory(placeType LocationType) (placeCategory POI.PlaceCategory) {
	switch placeType {
	case LocationTypePark, LocationTypeAmusementPark, LocationTypeGallery, LocationTypeMuseum:
		placeCategory = POI.PlaceCategoryVisit
	case LocationTypeCafe, LocationTypeRestaurant:
		placeCategory = POI.PlaceCategoryEatery
	default:
		placeCategory = POI.PlaceCategoryEatery
	}
	return
}

// refs: maps/examples/places/placedetails/placedetails.go
func parseFields(fields string) ([]maps.PlaceDetailsFieldMask, error) {
	var res []maps.PlaceDetailsFieldMask
	for _, s := range strings.Split(fields, ",") {
		f, err := maps.ParsePlaceDetailsFieldMask(s)
		utils.CheckErrImmediate(err, utils.LogError)
		res = append(res, f)
	}
	return res, nil
}

func logErr(err error, logLevel uint) bool {
	utils.CheckErrImmediate(err, logLevel)
	if err != nil {
		return true
	}
	return false
}
