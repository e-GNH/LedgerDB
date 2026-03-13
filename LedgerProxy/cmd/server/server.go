package main

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"strings"
	"path/filepath"

	"google.golang.org/grpc"
	// "google.golang.org/grpc/credentials/insecure"

	"LedgerDB/services/logging"
	pb "LedgerProxy/api"
	sec "LedgerProxy/modules/security"
	kernelpb "StorageKernel/proto/worldstate"
	batching "LedgerProxy/modules/batching"
)

var logger = logging.New("server", "./")

type securityServer struct {
	pb.UnimplementedSecurityServiceServer
	myPrivKey    *rsa.PrivateKey
	bankKeys     map[string]*rsa.PublicKey // TODO: make it a list of public keys
	kernelClient kernelpb.WorldStateServiceClient
}

func (s *securityServer) Execute(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {

	logger.Info("--> Received gRPC Secure() request")
	msg, ok := sec.VerifySecurity(req.EncryptedData, s.myPrivKey, s.bankKeys)

	if !ok {
		logger.Error("Security pipeline rejected the message")
		return &pb.SecureResponse{
			Success: false,
			Message: "Security pipeline rejected the message",
		}, nil
	}

	logger.Debug("SECURITY SUCCESS: Pipeline passed!")

	logger.Info("WAL in the local disk")
	batching.SaveBatchItem(msg)

	logger.Info("Adding transaction to world state...")
	// TODO: uncomment these
	// _, err := s.kernelClient.Transfer(ctx, &kernelpb.TransferRequest{
	// 	Nonce:  msg.Nonce,
	// 	FromId: msg.From,
	// 	ToId:   msg.To,
	// 	Amount: int64(msg.Amount),
	// })
	// if err != nil {
	// 	logger.Error(fmt.Sprintf("Kernel rejected transfer: %v", err))
	// 	return &pb.SecureResponse{
	// 		Success: false,
	// 		Message: fmt.Sprintf("transfer failed: %v", err),
	// 	}, nil
	// }
	return &pb.SecureResponse{
		Success: true,
		Message: "Transaction validated & successfully added to world state",
	}, nil
}


func loadBankKeysFromDir(dirPath string) map[string]*rsa.PublicKey {
    bankMap := make(map[string]*rsa.PublicKey)

    files, err := os.ReadDir(dirPath)
    if err != nil {
        logger.Error(fmt.Sprintf("Failed to read keys directory: %v", err))
        return bankMap
    }

    for _, file := range files {
        if !file.IsDir() && strings.HasSuffix(file.Name(), ".pem") {
            path := filepath.Join(dirPath, file.Name())
            keyBytes, err := os.ReadFile(path)
            if err != nil {
                logger.Error(fmt.Sprintf("Failed to read key file %s: %v", file.Name(), err))
                continue
            }

            pubKey := sec.ParsePublicKeyBytes(keyBytes)
            if pubKey != nil {
                bankName := strings.TrimSuffix(file.Name(), ".pem")
                bankMap[bankName] = pubKey
                logger.Info(fmt.Sprintf("Successfully loaded trusted key for bank: %s", bankName))
            }
        }
    }

    return bankMap
}

func main() {

	logger.Debug("Reading keys...")
	myPrivBytes, err := os.ReadFile("modules/security/keys/my_key")
	if err != nil {
		panic("Could not read my_key file")
	}

	trustedBankKeys := loadBankKeysFromDir("modules/security/keys/banks")

	logger.Info("Starting gRPC Security Server on port 50051...")
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}
	// kernelConn, err := grpc.Dial("localhost:50053", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// if err != nil {
	// 	panic(fmt.Sprintf("failed to connect to kernel: %v", err))
	// }
	// defer kernelConn.Close()
	grpcServer := grpc.NewServer()

	myServerInstance := &securityServer{
		myPrivKey:    sec.ParsePrivateKeyBytes(myPrivBytes),
		bankKeys: trustedBankKeys,
		// kernelClient: kernelpb.NewWorldStateServiceClient(kernelConn),
		kernelClient: nil,
	}

	pb.RegisterSecurityServiceServer(grpcServer, myServerInstance)

	logger.Info("gRPC Security Server is running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}
