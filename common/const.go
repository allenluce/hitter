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

	DBS = map[string]string{
		"local": "mongodb://localhost",
		"old":   "mongodb://mongo.lyfemobile.net:27017/rtbtest",
		"newdb": "mongodb://" +
			"svc_rc_test:testing123@ds141618-a0.mlab.com:41618,ds141618-a1.mlab.com:41618" +
			"/rtb?replicaSet=rs-ds141618",
	}

	MONGODB = map[string]string{
		"local": "rtbtest",
		"old":   "rtbtest",
		"newdb": "rtb",
	}
)
