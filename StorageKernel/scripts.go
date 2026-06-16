package main

import "github.com/redis/go-redis/v9"

var transferScript = redis.NewScript(`
	-- KEYS[1] = nonce key
	-- KEYS[2] = from account key
	-- KEYS[3] = to account key
	-- KEYS[4] = offline account boolean
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
	local balance_key = "balance"
	local pending_key = "pending"
	if KEYS[4] == "true" then
		balance_key = "offline"
		pending_key = "offline"
	end
	local balance = tonumber(redis.call("HGET", KEYS[2], balance_key) or "0")
	local sender_account_status = redis.call("HGET", KEYS[2], "status")
	if sender_account_status == "banned" then
		return {err="FROM_ACCOUNT_BANNED"}
	end
	local receiver_account_status = redis.call("HGET", KEYS[3], "status")
	if receiver_account_status == "banned" then
		return {err="TO_ACCOUNT_BANNED"}
	end
	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end

	redis.call("HINCRBY", KEYS[2], balance_key, -amount)
	redis.call("HINCRBY", KEYS[3], pending_key,  amount)
	redis.call("SET",     KEYS[1], 1)
	local seq = redis.call("INCR", "global:sequence")

	return {"OK", seq}
`)

var offlineDepositScript = redis.NewScript(`
	-- KEYS[1] = nonce key
	-- KEYS[2] = account key
	-- ARGV[1] = amount
	local amount  = tonumber(ARGV[1])
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end
	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end
	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "balance") or "0")

	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end

	redis.call("HINCRBY", KEYS[2], "balance", -amount)
	redis.call("HINCRBY", KEYS[2], "offline",  amount)
	redis.call("SET",     KEYS[1], 1)
	local seq = redis.call("INCR", "global:sequence")

	return {"OK", seq}
`)

var offlineWithdrawScript = redis.NewScript(`
	-- KEYS[1] = nonce key
	-- KEYS[2] = account key
	-- ARGV[1] = amount
	local amount  = tonumber(ARGV[1])
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end
	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end
	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "offline") or "0")

	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end

	redis.call("HINCRBY", KEYS[2], "balance", 	amount)
	redis.call("HINCRBY", KEYS[2], "offline",  -amount)
	redis.call("SET",     KEYS[1], 1)
	local seq = redis.call("INCR", "global:sequence")

	return {"OK", seq}
`)
var createAccountScript = redis.NewScript(`
	-- KEYS[1] = nonce key
	-- KEYS[2] = account key
	-- ARGV[1] = balance
	-- ARGV[2] = tier
	local balance  = tonumber(ARGV[1])
	local tier = ARGV[2]
	if balance < 0 then
		return {err="INVALID_BALANCE"}
	end
	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end
	if redis.call("EXISTS", KEYS[2]) == 1 then
		return {err="USER_ACCOUNT_ALREADY_EXISTS"}
	end
	redis.call("HSET", KEYS[2],
		"balance", balance,
		"pending", 0,
		"offline", 0,
		"tier", tier,
		"status", "active"
	)
	redis.call("SET",     KEYS[1], 1)
	local seq = redis.call("INCR", "global:sequence")

	return {"OK", seq}
`)

var changeAccountStatusScript = redis.NewScript(`
	-- KEYS[1] = account key
	-- ARGV[1] = status
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end
	local status = ARGV[1]
	if status ~= "active" and status ~= "banned" and status ~= "flagged" then
		return {err="INVALID_STATUS"}
	end
	redis.call("HSET", KEYS[1], "status", status)
	local seq = redis.call("INCR", "global:sequence")
	
	return {"OK", seq}
`)

var getAccountsTierScript = redis.NewScript(`
	-- KEYS[1] = sender account key
	-- KEYS[2] = receiver account key
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return {err="FROM_ACCOUNT_NOT_FOUND"}
	end
	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="TO_ACCOUNT_NOT_FOUND"}
	end

	local tiers = {}

	tiers[1] = redis.call("HGET", KEYS[1], "tier")
	tiers[2] = redis.call("HGET", KEYS[2], "tier")

	return tiers
`)

