package main

import "github.com/redis/go-redis/v9"

var commitTransferScript = redis.NewScript(`
	--KEYS[1] = nonce key
	--KEYS[2] = to account key
	--ARGV[1] = amount of transaction
	local amount = tonumber(ARGV[1])
	
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end

	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end

	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="TO_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "pending") or 0)

	if balance < amount then 
		return {err="INSUFFICIENT_FUNDS"}
	end

	redis.call("HINCRBY", KEYS[2], "pending", -amount)
	redis.call("HINCRBY", KEYS[2], "balance",  amount)
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`,
)

var transferScript = redis.NewScript(`
	--KEYS[1] = nonce key
	--KEYS[2] = from account key
	--KEYS[3] = to account key
	--KEYS[4] = offline boolean
	--ARGV[1] = amount of transaction

	local amount = tonumber(ARGV[1])
	
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


	local receiver_status = redis.call("HGET", KEYS[3], "status")
	if receiver_status == "banned" then
		return {err="TO_ACCOUNT_BANNED"}
	end

	if KEYS[2] ~= "account:BANK_ACCOUNT" then
		local sender_status = redis.call("HGET", KEYS[2], "status")
		if sender_status == "banned" then
			return {err="FROM_ACCOUNT_BANNED"}
		end
		local balance = tonumber(redis.call("HGET", KEYS[2], balance_key) or 0)

		if balance < amount then 
			return {err="INSUFFICIENT_FUNDS"}
		end
		redis.call("HINCRBY", KEYS[2], balance_key, -amount)
	end
	redis.call("HINCRBY", KEYS[3], pending_key,  amount)
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`)
var transferRollBackScript = redis.NewScript(`
	--KEYS[1] = nonce key
	--KEYS[2] = from account key
	--KEYS[3] = to account key
	--KEYS[4] = offline boolean
	--ARGV[1] = amount of transaction

	local amount = tonumber(ARGV[1])
	
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
		return {err="CANNOT_ROLLBACK_OFFLINE_TRANSFER"}
	end


	local receiver_status = redis.call("HGET", KEYS[3], "status")
	if receiver_status == "banned" then
		return {err="TO_ACCOUNT_BANNED"}
	end
	local balance = tonumber(redis.call("HGET", KEYS[3], pending_key) or 0)

	if balance < amount then 
		return {err="INSUFFICIENT_FUNDS"}
	end
	if KEYS[2] ~= "account:BANK_ACCOUNT" then
		local sender_status = redis.call("HGET", KEYS[2], "status")
		if sender_status == "banned" then
			return {err="FROM_ACCOUNT_BANNED"}
		end

		redis.call("HINCRBY", KEYS[2], balance_key, amount)
	end
	redis.call("HINCRBY", KEYS[3], pending_key,  -amount)
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`)

var offlineDepositScript = redis.NewScript(`
	--KEYS[1] = nonce key
	--KEYS[2] = account key
	--ARGV[1] = amount
	local amount = tonumber(ARGV[1])
	
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end

	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end

	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "balance") or 0)

	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end


	redis.call("HINCRBY", KEYS[2], "balance",  -amount)
	redis.call("HINCRBY", KEYS[2], "offline",  amount)
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`)

var offlineWithdrawScript = redis.NewScript(`
	--KEYS[1] = nonce key
	--KEYS[2] = account key
	--ARGV[1] = amount
	local amount = tonumber(ARGV[1])
	
	if amount <= 0 then
		return {err="INVALID_AMOUNT"}
	end

	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end

	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end

	local balance = tonumber(redis.call("HGET", KEYS[2], "offline") or 0)

	if balance < amount then
		return {err="INSUFFICIENT_FUNDS"}
	end


	redis.call("HINCRBY", KEYS[2], "balance",  amount)
	redis.call("HINCRBY", KEYS[2], "offline",  -amount)
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`)

var createAccountScript = redis.NewScript(`
	-- KEYS[1]: nonce
	-- KEYS[2]: account
	-- KEYS[3]: merchant name key (optional)
	-- ARGV[1]: balance
	-- ARGV[2]: tier
	
	local balance = tonumber(ARGV[1])

	if balance <= 0 then
		return {err="INVALID_BALANCE"}
	end

	if redis.call("EXISTS", KEYS[1]) == 1 then
		return {err="NONCE_ALREADY_USED"}
	end
	if redis.call("EXISTS", KEYS[2]) == 1 then
		return {err="USER_ACCOUNT_ALREADY_EXISTS"}
	end
	redis.call("HSET", KEYS[2], "balance", ARGV[1])
	redis.call("HSET", KEYS[2], "offline", "0")
	redis.call("HSET", KEYS[2], "tier", ARGV[2])
	redis.call("HSET", KEYS[2], "pending", 0)
	redis.call("HSET", KEYS[2], "status", "active")

	if ARGV[2] == "MERCHANT" then
		if redis.call("EXISTS", KEYS[3]) == 1 then
			return {err="MERCHANT_NAME_ALREADY_EXISTS"}
		end
		redis.call("SET", KEYS[3], KEYS[2])
	end
	redis.call("SET", KEYS[1], "1")

	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}
`)
var changeAccountStatusScript = redis.NewScript(`
	--KEYS[1] = account key
	--ARGV[1] = status
	--ARGV[2] = reason for level 2 checks
	--ARGV[3] = score of machine learning model
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end
	redis.call("HSET", KEYS[1], "status", ARGV[1])
	if ARGV[2] ~= "" then
		redis.call("HSET", KEYS[1], "reason", ARGV[2])
	end
	if ARGV[3] ~= "-1" then
		redis.call("HSET", KEYS[1], "score", ARGV[3])
	end
	if ARGV[1] == "active" then 
		redis.call("HDEL", KEYS[1], "score")
		redis.call("HDEL", KEYS[1], "reason")
	end
	local seq = redis.call("INCR", "global:sequence")
	return{ "OK", seq}

`)
var getMerchantAccountIdScript = redis.NewScript(`
	-- KEYS[1] = merchant name
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return {err="USER_ACCOUNT_NOT_FOUND"}
	end
	local account = redis.call("GET", KEYS[1])
	return account
`)

var getAccountsTierScript = redis.NewScript(`
	--KEYS[1] = sender account
	--KEYS[2] = receiver account
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return {err="FROM_ACCOUNT_NOT_FOUND"}
	end

	if redis.call("EXISTS", KEYS[2]) == 0 then
		return {err="TO_ACCOUNT_NOT_FOUND"}
	end
	local tiers = {}
	local sender_tier = redis.call("HGET", KEYS[1], "tier")
	local receiver_tier = redis.call("HGET", KEYS[2], "tier")
	
	tiers[1] = sender_tier
	tiers[2] = receiver_tier
	return tiers
`)
