package common

import mgo "gopkg.in/mgo.v2"

const (
	TdColl         = "total_data"
	LocColl        = "location_data"
	DeviceColl     = "device_data"
	AdvertiserColl = "advertiser"
	CampaignColl   = "campaign"
)

var (
	WHICHDB    = "newdb"
	DBSESSIONS = map[string]*mgo.Session{}
)
