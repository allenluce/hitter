// +build test

package common

var (
	DBS = map[string]string{
		"local": "mongodb://localhost",
		"old":   "mongodb://mongo.lyfemobile.net:27017/rtbtest",
		"newdb": "mongodb://" +
			"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-00-ptf9c.mongodb.net," +
			"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-01-ptf9c.mongodb.net," +
			"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-02-ptf9c.mongodb.net" +
			"?replicaSet=RhythmContent-shard-0",
	}

	MONGODB = "rtbtest"
	WEBPORT = 8088
)
