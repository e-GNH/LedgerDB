package main

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"LedgerDB/services/logging"
	pb "LedgerProxy/api"
	"LedgerProxy/types"
)

var logger = logging.New("client", "./")

func main() {
	fmt.Println("=== Starting gRPC Security Client ===")

	// =========================================================================
	// 1. READ REQUIRED KEYS
	// =========================================================================

	// A. Read the Client's OWN Private Key (used to SIGN the message)
	senderPrivBytes, err := os.ReadFile("modules/security/keys/banks/CIB")
	if err != nil {
		panic("Could not read CIB file. Make sure it exists!")
	}
	senderPrivKey := parsePrivateKey(string(senderPrivBytes))

	receiverPubBytes, err := os.ReadFile("modules/security/keys/my_key.pem")
	if err != nil {
		panic("Could not read my_key.pem file. Make sure it exists!")
	}
	receiverPubKey := parsePublicKey(receiverPubBytes)

	fmt.Println("Successfully loaded Client Private Key and Server Public Key.")

	// payload := types.SecureMessage{
	// 	Timestamp: time.Now(),
	// 	From:      "A",
	// 	To:        "B",
	// 	Amount:    11,
	// 	Message:   "Payment for cloud infrastructure",
	// 	Nonce:     "unique-txn-123s245",
	// }

	payload := types.SecureOfflineWithdrawMessage{
		Amount:    11,
		Nonce:     "unique-txn-123s245",
		AccountId: "A",
		Timestamp: time.Now(),
	}
	payloadBytes, _ := json.Marshal(payload)

	hashRaw := sha256.Sum256(payloadBytes)
	hash := hashRaw[:]

	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, hash)
	if err != nil {
		panic(fmt.Sprintf("Failed to sign payload: %v", err))
	}

	clientPubKey := &senderPrivKey.PublicKey
	pubBytes, err := x509.MarshalPKIXPublicKey(clientPubKey)
	if err != nil {
		panic("failed to serialize public key")
	}

	// Pack the message
	unpackedMsg := types.UnpackedMessage{
		Data:       payloadBytes,
		Signature:  signature,
		Hash:       hash,
		BankPubKey: pubBytes, // The client's public key (to prove who signed it)
	}
	unpackedBytes, _ := json.Marshal(unpackedMsg)

	fmt.Println("Encrypting payload...")

	aesKey := make([]byte, 32)
	rand.Read(aesKey)

	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	aesCiphertext := gcm.Seal(nonce, nonce, unpackedBytes, nil)

	// Encrypt the AES key using the SERVER'S Public Key
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, receiverPubKey, aesKey, nil)
	if err != nil {
		panic(fmt.Sprintf("Failed to encrypt AES key: %v", err))
	}

	encryptedData := append(encryptedAESKey, aesCiphertext...)

	fmt.Printf("Encrypted payload size: %d bytes\n", len(encryptedData))

	fmt.Println("Connecting to gRPC server at localhost:50001...")

	conn, err := grpc.Dial("localhost:50001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("Did not connect: %v", err))
	}
	defer conn.Close()

	client := pb.NewSecurityServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Sending Secure() request over the network...")
	req := &pb.SecureRequest{EncryptedData: encryptedData}

	res, err := client.Execute(ctx, req)
	if err != nil {
		panic(fmt.Sprintf("Error calling Secure RPC: %v", err))
	}

	fmt.Println("\n--- Server Response ---")
	if res.Success {
		fmt.Println("✅ Transaction Validated by Server!")
	} else {
		fmt.Println("❌ Server Rejected Transaction!")
		fmt.Printf("   Reason: %s\n", res.Message)
	}
}

func parsePrivateKey(pemStr string) *rsa.PrivateKey {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		panic("failed to parse PEM block containing the private key")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse private key: %v", err))
	}
	return priv
}

func parsePublicKey(pemBytes []byte) *rsa.PublicKey {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		panic("failed to parse PEM block containing the public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		if rsaPub, ok := pub.(*rsa.PublicKey); ok {
			return rsaPub
		}
	}
	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse public key: %v", err))
	}
	return rsaPub
}
