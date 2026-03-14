package main

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"LedgerDB/services/logging"
	pb "LedgerProxy/api"
	batching "LedgerProxy/modules/batching"
	sec "LedgerProxy/modules/security"
	// ts_store "StorageKernel/proto/TransactionsStore"
	kernelpb "StorageKernel/proto/worldstate"
	ledgerserverpb "LedgerServer/api"
)

var logger = logging.New("server", "../../")

type securityServer struct {
	pb.UnimplementedSecurityServiceServer
	myPrivKey               *rsa.PrivateKey
	bankKeys                map[string]*rsa.PublicKey // TODO: make it a list of public keys
	kernelClient            kernelpb.WorldStateServiceClient
	LedgerServerClient ledgerserverpb.TransactionsServiceClient
}

func (s *securityServer) Execute(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {

	logger.Info("--> Received gRPC Secure() request")
	msg, ok := sec.VerifySecurity(req.EncryptedData, s.myPrivKey, s.bankKeys)
	
	if msg != nil {
		msg.Status = ok
	}
	

	if ok {
		logger.Info("SECURITY SUCCESS: Pipeline passed!")
	}
	message := "Transaction rejected due to security"


	logger.Info("WAL in the local disk")
	batching.SaveBatchItem(msg, s.LedgerServerClient)

	if ok {
		logger.Info("Passing transaction to world state...")
		// TODO: tell zeyad that even if fail, it has to be committed
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
		message = "Transaction validated & successfully added to world state"
	}

	return &pb.SecureResponse{
		Success: ok,
		Message: message, 
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

	logger.Info("Reading keys...")
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
	StoreConn, err := grpc.Dial("localhost:50053", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to kernel: %v", err))
	}
	defer StoreConn.Close()
	grpcServer := grpc.NewServer()

	myServerInstance := &securityServer{
		myPrivKey: sec.ParsePrivateKeyBytes(myPrivBytes),
		bankKeys:  trustedBankKeys,
		// kernelClient: kernelpb.NewWorldStateServiceClient(kernelConn),
		kernelClient:            nil,
		LedgerServerClient: ledgerserverpb.NewTransactionsServiceClient(StoreConn),
	}

	pb.RegisterSecurityServiceServer(grpcServer, myServerInstance)

	logger.Info("gRPC Security Server is running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}
