package main

import (
	"sync"

	"LedgerProxy/modules/batching"
	pb "LedgerProxy/api"
)

type bankStream struct {
	stream pb.ReceiptService_SubscribeServer
	done   chan struct{}
}

// Send implements batching.SendStream
func (bs *bankStream) Send(receipt *pb.TransactionReceipt) error {
	return bs.stream.Send(receipt)
}

type BankRegistry struct {
	mu      sync.RWMutex
	streams map[string]*bankStream
}

func NewBankRegistry() *BankRegistry {
	return &BankRegistry{
		streams: make(map[string]*bankStream),
	}
}

func (r *BankRegistry) Register(prefix string, stream pb.ReceiptService_SubscribeServer) *bankStream {
	r.mu.Lock()
	defer r.mu.Unlock()

	bs := &bankStream{
		stream: stream,
		done:   make(chan struct{}),
	}
	r.streams[prefix] = bs
	return bs
}

func (r *BankRegistry) Unregister(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, prefix)
}

// Get implements batching.Registry
func (r *BankRegistry) Get(prefix string) batching.SendStream {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bs := r.streams[prefix]
	if bs == nil {
		return nil
	}
	return bs
}