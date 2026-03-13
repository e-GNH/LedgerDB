package main

import "github.com/redis/go-redis/v9"

var transferScript = redis.NewScript(`
	-- KEYS[1] = nonce key
	-- KEYS[2] = from account key
	-- KEYS[3] = to account key
	-- ARGV[1] = amount
	local amount  = tonumber(ARGV[1])
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end
	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end
	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="FROM_ACCOUNT_NOT_FOUND"}
	end
	if redis.call("EXISTS", KEYS[3]) == 0 then
		return {err="TO_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "balance") or "0")

	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end

	redis.call("HINCRBY", KEYS[2], "balance", -amount)
	redis.call("HINCRBY", KEYS[3], "pending",  amount)
	redis.call("SET",     KEYS[1], 1)

	return "OK"
`)