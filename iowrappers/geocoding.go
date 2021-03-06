package iowrappers

import (
	"context"
	"errors"
	"github.com/weihesdlegend/Vacation-planner/utils"
	"googlemaps.github.io/maps"
)

// Translate city, country to its central location
func (mapsClient MapsClient) GetGeocode(query *GeocodeQuery) (lat float64, lng float64, err error) {
	req := &maps.GeocodingRequest{
		Components: map[maps.Component]string{
			maps.ComponentLocality: query.City,
			maps.ComponentCountry:  query.Country,
		}}

	resp, err := mapsClient.client.Geocode(context.Background(), req)
	if err != nil {
		utils.CheckErrImmediate(err, utils.LogError)
		return
	}

	if len(resp) < 1 {
		err = errors.New("maps geo-coding response invalid")
		utils.CheckErrImmediate(err, utils.LogError)
		return
	}

	location := resp[0].Geometry.Location
	lat = location.Lat
	lng = location.Lng

	cityName := resp[0].AddressComponents[0].LongName
	query.City = cityName

	return
}
