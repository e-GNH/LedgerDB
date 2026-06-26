package main

import (
	"context"

	"github.com/colinmarc/hdfs/v2"
	"github.com/redis/go-redis/v9"
)

func newRedisClient() *redis.Client {
	file_name = "client.go"
	logger.Info(" - [" + file_name + "] - Creating Redis Client")
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // TODO: to be to env
		Password: "", // no password
		DB:       0,  // use default DB
		Protocol: 2,
	})

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil
	}
	logger.Info(" - [" + file_name + "] - Redis Client Created")
	return rdb
}

func newHDFSClient() (*hdfs.Client, error) {
	file_name = "client.go"
	logger.Info(" - [" + file_name + "] - Creating HDFS Client")
	client, err := hdfs.New("localhost:9000") // TODO: to be to env
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, err
	}
	logger.Info(" - [" + file_name + "] - HDFS Client Created")
	return client, nil
}
