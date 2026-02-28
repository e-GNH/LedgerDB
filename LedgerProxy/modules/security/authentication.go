package security

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"
	"os"

	"LedgerDB/services/logging"
	"LedgerProxy/types"
)

var log = logging.New("security/authentication", "./")

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
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			panic("public key is not of type RSA")
		}
		return rsaPub
	}

	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse public key as either PKIX or PKCS1: %v", err))
	}

	return rsaPub
}
func ProcessMessage(encryptedData []byte, myPrivKey *rsa.PrivateKey, senderPubKey *rsa.PublicKey) (*types.SecureMessage, bool) {
	log.Debug("Started Processing")

	decryptedBytes, err := HybridDecrypt(encryptedData, myPrivKey)
	if err != nil {
		log.Error(fmt.Sprintf("Failed to decrypt data: %v", err))
		return nil, false
	}
	log.Debug("Decrypted data")

	var unpacked types.UnpackedMessage
	if err := json.Unmarshal(decryptedBytes, &unpacked); err != nil {
		log.Error(fmt.Sprintf("Failed to unmarshal decrypted data: %v", err))
		return nil, false
	}
	log.Debug("Unpacked data")

	if !VerifyUserSignature(unpacked.Hash, unpacked.Signature, senderPubKey) {
		log.Error("Failed to verify user signature")
		return nil, false
	}
	log.Debug("Verified user signature")

	if !CalculateAndHashCheck(unpacked.Data, unpacked.Hash) {
		log.Error("Hash check failed: Data integrity compromised")
		return nil, false
	}
	log.Debug("Hash check passed")

	var msg types.SecureMessage
	if err := json.Unmarshal(unpacked.Data, &msg); err != nil {
		log.Error(fmt.Sprintf("Failed to unmarshal message data: %v", err))
		return nil, false
	}
	log.Debug("Unpacked message data")

	if !VerifyTimeliness(msg.Timestamp) {
		log.Error("Message failed timeliness check: Possible replay attack")
		return nil, false
	}
	log.Debug("Message is timely")

	return &msg, true
}

func HybridDecrypt(encryptedData []byte, myPrivKey *rsa.PrivateKey) ([]byte, error) {
	if len(encryptedData) < 256 {
		return nil, fmt.Errorf("ciphertext too short")
	}

	encryptedAESKey := encryptedData[:256]
	aesCiphertext := encryptedData[256:]

	aesKey, err := rsa.DecryptOAEP(sha256.New(), nil, myPrivKey, encryptedAESKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt AES key: %v", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
    }

	nonceSize := gcm.NonceSize()
	if len(aesCiphertext) < nonceSize {
		return nil, fmt.Errorf("aes ciphertext too short")
	}

	nonce, ciphertext := aesCiphertext[:nonceSize], aesCiphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func VerifyUserSignature(hash []byte, signature []byte, senderPubKey *rsa.PublicKey) bool {
	return rsa.VerifyPKCS1v15(senderPubKey, crypto.SHA256, hash, signature) == nil
}

func CalculateAndHashCheck(data []byte, providedHash []byte) bool {
	calculatedHash := sha256.Sum256(data)
	return bytes.Equal(calculatedHash[:], providedHash)
}

func VerifyTimeliness(msgTimestamp time.Time) bool {
	log.Debug(fmt.Sprintf("Message timestamp: %s", msgTimestamp.Format(time.RFC3339)))
	now := time.Now()
	diff := now.Sub(msgTimestamp)

	if diff < 0 {
		diff = -diff
	}

	return diff <= 5*time.Minute
}



func secure() bool {
	
	log.Info("Reading keys...")
	myPrivBytes, err := os.ReadFile("keys/my_key")
	if err != nil {
		log.Error(fmt.Sprintf("Could not read my_key file: %v", err))
		return false
	}
	senderPubBytes, err := os.ReadFile("keys/sender_key_pub.pem")
	if err != nil {
		log.Error(fmt.Sprintf("Could not read sender_key_pub.pem file: %v", err))
		return false
	}

	log.Debug("Started Parsing.")
	myPrivKey := parsePrivateKey(string(myPrivBytes))
	senderPubKey := parsePublicKey(string(senderPubBytes))
	log.Debug("Finished Parsing.")

	log.Info("Reading incoming encrypted data...")
	encryptedData, err := os.ReadFile("encrypted_payload.bin")
	if err != nil {
		log.Error(fmt.Sprintf("Failed to read encrypted_payload.bin: %v", err))
		return false
	}

	log.Info("Passing data to ProcessMessage()...")
	_, ok := ProcessMessage(encryptedData, myPrivKey, senderPubKey) // to be used final message

	if ok {
		log.Info("SUCCESS! Message verified, decrypted, and timely.")
	} else {
		log.Debug("SECURITY ALERT: Pipeline failed!")
		return false
	}

	return true
}