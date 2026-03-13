package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"LedgerDB/services/logging"
	pb "LedgerProxy/api"
	"LedgerProxy/modules/security"
	kernelpb "StorageKernel/proto/worldstate"
)

var log = logging.New("server", "./")


type securityServer struct {
	pb.UnimplementedSecurityServiceServer
	myPrivKey    *rsa.PrivateKey
	senderPubKey *rsa.PublicKey
	kernelClient kernelpb.WorldStateServiceClient
}

func (s *securityServer) Execute(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {

	log.Info("--> Received gRPC Secure() request")
	msg, ok := security.VerifySecurity(req.EncryptedData, s.myPrivKey, s.senderPubKey)

	if !ok {
		log.Error("Security pipeline rejected the message")
		return &pb.SecureResponse{
			Success: false,
			Message: "Security pipeline rejected the message",
		}, nil
	}

	log.Debug("SECURITY SUCCESS: Pipeline passed!")

	_, err := s.kernelClient.Transfer(ctx, &kernelpb.TransferRequest{
		Nonce:  msg.Nonce,
		FromId: msg.From,
		ToId:   msg.To,
		Amount: int64(msg.Amount),
	})
	if err != nil {
		log.Error(fmt.Sprintf("Kernel rejected transfer: %v", err))
		return &pb.SecureResponse{
			Success: false,
			Message: fmt.Sprintf("transfer failed: %v", err),
		}, nil
	}
	return &pb.SecureResponse{
		Success: true,
		Message: "Transaction validated & successfully added to world state",
	}, nil
}

func parsePrivateKey(pemBytes []byte) *rsa.PrivateKey {
	log.Debug("Parsing private key...")
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		log.Error("Failed to parse PEM block containing the private key")
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

func main() {

	log.Debug("Reading keys...")
	myPrivBytes, err := os.ReadFile("modules/security/keys/my_key")
	if err != nil {
		panic("Could not read my_key file")
	}

	senderPubBytes, err := os.ReadFile("modules/security/keys/sender_key_pub.pem")
	if err != nil {
		panic("Could not read sender_key_pub.pem file")
	}

	log.Info("Starting gRPC Security Server on port 50051...")
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}
	kernelConn, err := grpc.Dial("localhost:50053", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to kernel: %v", err))
	}
	defer kernelConn.Close()
	grpcServer := grpc.NewServer()

	myServerInstance := &securityServer{
		myPrivKey:    parsePrivateKey(myPrivBytes),
		senderPubKey: parsePublicKey(senderPubBytes),
		kernelClient: kernelpb.NewWorldStateServiceClient(kernelConn),
	}

	pb.RegisterSecurityServiceServer(grpcServer, myServerInstance)

	log.Info("gRPC Security Server is running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}
