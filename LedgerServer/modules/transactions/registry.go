package transactions

import (
	"sync"

	pb "LedgerServer/api"
)

// bankStream holds the active stream for a subscribed bank
type bankStream struct {
	stream pb.ReceiptService_SubscribeServer
	done   chan struct{}
}

// BankRegistry manages active bank subscriptions keyed by wallet prefix
type BankRegistry struct {
	mu      sync.RWMutex
	streams map[string]*bankStream // key: bank prefix e.g. "000"
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

// Get returns the active stream for a prefix, nil if not subscribed
func (r *BankRegistry) Get(prefix string) *bankStream {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.streams[prefix]
}
