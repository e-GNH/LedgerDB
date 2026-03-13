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

	"LedgerDB/services/logging"
	"LedgerProxy/types"
)

var log = logging.New("security/authentication", "./")

func ParsePrivateKey(pemStr string) *rsa.PrivateKey {
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

func ParsePrivateKeyBytes(pemStr []byte) *rsa.PrivateKey {
	block, _ := pem.Decode(pemStr)
	if block == nil {
		panic("failed to parse PEM block containing the private key")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse private key: %v", err))
	}
	return priv
}



func ParsePublicKey(pemStr string) *rsa.PublicKey {
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

func ParsePublicKeyBytes(pemStr []byte) *rsa.PublicKey {
	block, _ := pem.Decode(pemStr)
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

func isKeyTrusted(received *rsa.PublicKey, trusted map[string]*rsa.PublicKey) bool {
    for _, trustedKey := range trusted {
        if received.N.Cmp(trustedKey.N) == 0 && received.E == trustedKey.E {
            return true
        }
    }
    return false
}

func VerifySecurity(encryptedData []byte, myPrivKey *rsa.PrivateKey, trustedKeys map[string]*rsa.PublicKey) (*types.SecureMessage, bool) {
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
	receivedPubKey := ParsePublicKeyBytes(unpacked.BankPubKey)
    if receivedPubKey == nil {
        log.Error("Received invalid public key format")
        return nil, false
    }

	if !isKeyTrusted(receivedPubKey, trustedKeys) {
        log.Error("Access Denied: The public key provided in the request is not in the trusted list.")
        return nil, false
    }
    log.Debug("Sender's public key is verified and trusted")

	if !VerifyUserSignature(unpacked.Hash, unpacked.Signature, receivedPubKey) {
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