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
	"io"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "LedgerProxy/api"
	"LedgerProxy/types"
)

func main() {
	fmt.Println("=== Full Pipeline Test ===")

	senderPrivBytes, err := os.ReadFile("/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/banks/CIB")
	if err != nil {
		panic("Could not read CIB private key")
	}
	senderPrivKey := parsePrivateKey(string(senderPrivBytes))

	receiverPubBytes, err := os.ReadFile("/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/my_key.pem")
	if err != nil {
		panic("Could not read server public key")
	}
	receiverPubKey := parsePublicKey(receiverPubBytes)

	fmt.Println("[OK] Keys loaded")

	proxyConn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to LedgerProxy: %v", err))
	}
	defer proxyConn.Close()

	receiptClient := pb.NewReceiptServiceClient(proxyConn)

	// Two different banks subscribing
	go subscribeReceipts(receiptClient, "000")
	go subscribeReceipts(receiptClient, "001")

	time.Sleep(500 * time.Millisecond)

	// payload := types.SecureMessage{
	// 	Timestamp: time.Now(),
	// 	From:      "000_wallet_A",
	// 	To:        "001_wallet_B",
	// 	Amount:    10,
	// 	Message:   "Payment for cloud infrastructure",
	// 	Nonce:     fmt.Sprintf("nonce-%d", time.Now().UnixNano()),
	// }

	payload := types.SecureOfflineWithdrawMessage{
		Timestamp: time.Now(),
		Amount:    10,
		Nonce:     fmt.Sprintf("nonce-%d", time.Now().UnixNano()),
	}
	payloadBytes, _ := json.Marshal(payload)

	hashRaw := sha256.Sum256(payloadBytes)
	hash := hashRaw[:]

	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, hash)
	if err != nil {
		panic(fmt.Sprintf("Failed to sign payload: %v", err))
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&senderPrivKey.PublicKey)
	if err != nil {
		panic("Failed to serialize public key")
	}

	unpackedMsg := types.UnpackedMessage{
		Data:       payloadBytes,
		Signature:  signature,
		Hash:       hash,
		BankPubKey: pubBytes,
	}
	unpackedBytes, _ := json.Marshal(unpackedMsg)

	aesKey := make([]byte, 32)
	rand.Read(aesKey)

	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	aesCiphertext := gcm.Seal(nonce, nonce, unpackedBytes, nil)

	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, receiverPubKey, aesKey, nil)
	if err != nil {
		panic(fmt.Sprintf("Failed to encrypt AES key: %v", err))
	}

	encryptedData := append(encryptedAESKey, aesCiphertext...)
	fmt.Printf("[OK] Payload encrypted (%d bytes)\n", len(encryptedData))

	secClient := pb.NewSecurityServiceClient(proxyConn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("[..] Sending transaction to LedgerProxy...")
	res, err := secClient.Execute(ctx, &pb.SecureRequest{EncryptedData: encryptedData})
	if err != nil {
		panic(fmt.Sprintf("Execute RPC failed: %v", err))
	}

	fmt.Println("\n--- Proxy Response ---")
	if res.Success {
		fmt.Println("✅ Transaction accepted by LedgerProxy")
	} else {
		fmt.Printf("❌ Transaction rejected: %s\n", res.Message)
	}

	// Wait long enough for both banks to receive their receipts
	fmt.Println("\n[..] Waiting for receipts on both banks...")
	time.Sleep(time.Second)
}

func subscribeReceipts(client pb.ReceiptServiceClient, prefix string) {
	ctx := context.Background()

	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{BankPrefix: prefix})
	if err != nil {
		fmt.Printf("[ERROR] Bank %s failed to subscribe: %v\n", prefix, err)
		return
	}

	fmt.Printf("[OK] Bank %s subscribed, waiting for receipts...\n", prefix)

	for {
		receipt, err := stream.Recv()
		if err == io.EOF {
			fmt.Printf("[INFO] Bank %s stream closed by server\n", prefix)
			return
		}
		if err != nil {
			fmt.Printf("[ERROR] Bank %s stream error: %v\n", prefix, err)
			return
		}

		fmt.Printf("\n--- Receipt [Bank %s] ---\n", prefix)
		fmt.Printf("  Hash:      %s\n", receipt.Hash)
		fmt.Printf("  Status:    %v\n", receipt.Status)
		fmt.Printf("  From:      %s\n", receipt.FromWallet)
		fmt.Printf("  To:        %s\n", receipt.ToWallet)
		fmt.Printf("  Amount:    %v\n", receipt.Amount)
		fmt.Printf("  Message:   %s\n", receipt.Message)
		fmt.Printf("  Timestamp: %s\n", receipt.TimeStamp)
		fmt.Println("------------------------")
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