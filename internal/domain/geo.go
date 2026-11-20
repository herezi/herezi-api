package domain

import "math"

const earthRadiusM = 6371008.8

// DistanceM returns the great-circle distance in meters (haversine).
// Used by the in-memory store; PostGIS uses ST_Distance on geography.
func DistanceM(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}
