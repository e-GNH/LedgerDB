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

	"LedgerProxy/api"
	"LedgerProxy/types"
)

func main() {
	fmt.Println("=== Starting gRPC Security Client ===")

	// =========================================================================
	// 1. READ REQUIRED KEYS
	// The client needs THEIR private key (to sign) and YOUR public key (to encrypt)
	// =========================================================================
	senderPrivBytes, err := os.ReadFile("modules/security/keys/sender_key")
	if err != nil {
		panic("Could not read sender_key file")
	}
	receiverPubBytes, err := os.ReadFile("modules/security/keys/my_key_pub.pem")
	if err != nil {
		panic("Could not read my_key_pub.pem file")
	}

	senderPrivKey := parsePrivateKey(string(senderPrivBytes))
	receiverPubKey := parsePublicKey(string(receiverPubBytes))

	// =========================================================================
	// 2. CREATE AND SIGN THE PAYLOAD
	// =========================================================================
	payload := types.SecureMessage{
		Timestamp: time.Now(),
		From:      "A",
		To:        "B",
		Amount:    12, // Let's send a new amount to prove it works!
		Message:   "Payment for cloud infrastructure",
		Nonce:     "unique-txn-12345",
	}
	payloadBytes, _ := json.Marshal(payload)

	hashRaw := sha256.Sum256(payloadBytes)
	hash := hashRaw[:]

	signature, _ := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, hash)

	unpackedMsg := types.UnpackedMessage{
		Data:      payloadBytes,
		Signature: signature,
		Hash:      hash,
	}
	unpackedBytes, _ := json.Marshal(unpackedMsg)

	// =========================================================================
	// 3. HYBRID ENCRYPTION (AES + RSA)
	// =========================================================================
	fmt.Println("Encrypting payload...")

	aesKey := make([]byte, 32)
	rand.Read(aesKey)

	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	aesCiphertext := gcm.Seal(nonce, nonce, unpackedBytes, nil)

	encryptedAESKey, _ := rsa.EncryptOAEP(sha256.New(), rand.Reader, receiverPubKey, aesKey, nil)
	encryptedData := append(encryptedAESKey, aesCiphertext...)

	fmt.Printf("Encrypted payload size: %d bytes\n", len(encryptedData))
	// =========================================================================
	// 4. SEND VIA gRPC
	// =========================================================================
	fmt.Println("Connecting to gRPC server at localhost:50051...")

	// Create an insecure connection (since we are on localhost and already encrypting the payload)
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("Did not connect: %v", err))
	}
	defer conn.Close()

	client := pb.NewSecurityServiceClient(conn)

	// Create a context with a 5-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Sending Secure() request over the network...")
	// corruptedData := append([]byte(nil), encryptedData...) 
	// corruptedData[len(corruptedData)-1] ^= 0xff
	// Send completely invalid bytes
	req := &pb.SecureRequest{EncryptedData: encryptedData}
	// req := &pb.SecureRequest{EncryptedData: []byte("this is not valid encrypted data")}}

	
	res, err := client.Execute(ctx, req)
	if err != nil {
		panic(fmt.Sprintf("Error calling Secure RPC: %v", err))
	}

	// =========================================================================
	// 5. PRINT THE SERVER'S RESPONSE
	// =========================================================================
	fmt.Println("\n--- Server Response ---")
	if res.Success {
		fmt.Println("✅ Transaction Validated by Server!")
	} else {
		fmt.Println("❌ Server Rejected Transaction!")
		fmt.Printf("   Reason: %s\n", res.Message)
	}
}

// --- Helper Functions (Same as before) ---

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

func parsePublicKey(pemStr string) *rsa.PublicKey {
	block, _ := pem.Decode([]byte(pemStr))
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