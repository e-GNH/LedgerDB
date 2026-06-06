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
	"LedgerProxy/types"

	ledgerserverpb "LedgerServer/api"
	kernelpb "StorageKernel/proto/worldstate"
)

var logger = logging.New("server", "../../")

type securityServer struct {
	pb.UnimplementedSecurityServiceServer
	pb.UnimplementedReceiptServiceServer
	myPrivKey          *rsa.PrivateKey
	bankKeys           map[string]*rsa.PublicKey
	kernelClient       kernelpb.WorldStateServiceClient
	LedgerServerClient ledgerserverpb.TransactionsServiceClient
	registry           *BankRegistry
}


func (s *securityServer) Subscribe(req *pb.SubscribeRequest, stream pb.ReceiptService_SubscribeServer) error {
	prefix := req.BankPrefix
	if prefix == "" {
		return fmt.Errorf("bank_prefix is required")
	}

	logger.Info(fmt.Sprintf("Bank subscribed with prefix: %s", prefix))
	bs := s.registry.Register(prefix, stream)

	select {
	case <-stream.Context().Done():
		logger.Info(fmt.Sprintf("Bank %s disconnected", prefix))
	case <-bs.done:
		logger.Info(fmt.Sprintf("Bank %s stream closed by server", prefix))
	}

	s.registry.Unregister(prefix)
	return nil
}


func (s *securityServer) Execute(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {
	logger.Info("--> Received gRPC Secure() request")

	msg, ok := sec.VerifySecurity[types.SecureMessage](req.EncryptedData, s.myPrivKey, s.bankKeys)
	if msg != nil {
		msg.Status = ok
	}

	if ok {
		logger.Info("SECURITY SUCCESS: Pipeline passed!")
	}

	message := "Transaction rejected due to security"

	if msg == nil {
		return &pb.SecureResponse{Success: false, Message: message}, nil
	}

	logger.Info("WAL: writing to local disk")
	if err := batching.SaveBatchItem(msg, s.LedgerServerClient); err != nil {
		logger.Error(fmt.Sprintf("Failed to save batch item: %v", err))
	}

	if ok {
		logger.Info("Passing transaction to world state...")
		_, err := s.kernelClient.Transfer(ctx, &kernelpb.TransferRequest{
			Nonce:  msg.Nonce,
			FromId: msg.From,
			ToId:   msg.To,
			Amount: int64(msg.Amount),
		})
		if err != nil {
			logger.Error(fmt.Sprintf("Kernel rejected transfer: %v", err))
			msg.Status = false
			og_message := msg.Message
			msg.Message = "Undo transaction with Nonce " + msg.Nonce 
			batching.SaveBatchItem(msg, s.LedgerServerClient)
			msg.Message = og_message
			batching.StreamReceipt(msg)
			return &pb.SecureResponse{
				Success: false,
				Message: fmt.Sprintf("transfer failed: %v", err),
			}, nil
		}
		msg.Status = true
		batching.StreamReceipt(msg)

		message = "Transaction validated & successfully added to world state"
	}

	return &pb.SecureResponse{Success: ok, Message: message}, nil
}

func (s *securityServer) OfflineWithdraw(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {
	logger.Info("--> Received Offline Withdraw() request")

	msg, ok := sec.VerifySecurity[types.SecureOfflineWithdrawMessage](req.EncryptedData, s.myPrivKey, s.bankKeys)
	if msg == nil || !ok {
		return &pb.SecureResponse{
			Success: false, 
			Message: "Failed to decrypt and verify message",
		}, nil
	}

	logger.Info("SECURITY SUCCESS: Pipeline passed!")

	// TODO: Solve this issue
	// logger.Info("WAL: writing to local disk")
	// if err := batching.SaveBatchItem(msg, s.LedgerServerClient); err != nil {
	// 	logger.Error(fmt.Sprintf("Failed to save batch item: %v", err))
	// }

	logger.Info("Passing transaction to world state...")
	resp, err := s.kernelClient.OfflineWithdraw(ctx, &kernelpb.OfflineWithdrawRequest{
		Nonce:  msg.Nonce,
		AccountId: msg.AccountId,
		Amount: int64(msg.Amount),
	})

	if err != nil {
		logger.Error(fmt.Sprintf("Kernel rejected transfer: %v", err))
	}
	
	return &pb.SecureResponse{Success: resp.GetOk(), Message: resp.GetMessage()}, nil
}


func (s *securityServer) OfflineDeposit(ctx context.Context, req *pb.SecureRequest) (*pb.SecureResponse, error) {
	logger.Info("--> Received Offline Deposit() request")

	msg, ok := sec.VerifySecurity[types.SecureOfflineDepositMessage](req.EncryptedData, s.myPrivKey, s.bankKeys)
	if msg == nil || !ok {
		return &pb.SecureResponse{
			Success: false, 
			Message: "Failed to decrypt and verify message",
		}, nil
	}

	logger.Info("SECURITY SUCCESS: Pipeline passed!")

	// TODO: Solve this issue
	// logger.Info("WAL: writing to local disk")
	// if err := batching.SaveBatchItem(msg, s.LedgerServerClient); err != nil {
	// 	logger.Error(fmt.Sprintf("Failed to save batch item: %v", err))
	// }

	logger.Info("Passing transaction to world state...")
	resp, err := s.kernelClient.OfflineDeposit(ctx, &kernelpb.OfflineDepositRequest{
		Nonce:  msg.Nonce,
		AccountId: msg.AccountId,
		Amount: int64(msg.Amount),
	})

	if err != nil {
		logger.Error(fmt.Sprintf("Kernel rejected transfer: %v", err))
	}
	
	return &pb.SecureResponse{Success: resp.GetOk(), Message: resp.GetMessage()}, nil
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
				logger.Info(fmt.Sprintf("Loaded trusted key for bank: %s", bankName))
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

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}

	kernelConn, err := grpc.Dial("localhost:50058", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to kernel: %v", err))
	}
	defer kernelConn.Close()

	storeConn, err := grpc.Dial("localhost:50053", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to LedgerServer: %v", err))
	}
	defer storeConn.Close()

	reg := NewBankRegistry()

	// Wire the registry into the batching package
	batching.SetRegistry(reg)

	grpcServer := grpc.NewServer()

	myServerInstance := &securityServer{
		myPrivKey:          sec.ParsePrivateKeyBytes(myPrivBytes),
		bankKeys:           trustedBankKeys,
		kernelClient:       kernelpb.NewWorldStateServiceClient(kernelConn),
		LedgerServerClient: ledgerserverpb.NewTransactionsServiceClient(storeConn),
		registry:           reg,
	}

	pb.RegisterSecurityServiceServer(grpcServer, myServerInstance)
	pb.RegisterReceiptServiceServer(grpcServer, myServerInstance)

	logger.Info("LedgerProxy running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}