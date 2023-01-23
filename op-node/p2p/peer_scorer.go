package p2p

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
)

const ConnectionFactor = -10

const PeerScoreThreshold = -100

type scorer struct {
	connGater ConnectionGater
	peerStore peerstore.Peerstore
	metricer  GossipMetricer
}

// Scorer is a peer scorer that scores peers based on application-specific metrics.
type Scorer interface {
	OnConnect()
	OnDisconnect()
	SnapshotHook() pubsub.ExtendedPeerScoreInspectFn
}

// NewScorer returns a new peer scorer.
func NewScorer(connGater ConnectionGater, peerStore peerstore.Peerstore, metricer GossipMetricer) Scorer {
	return &scorer{
		connGater: connGater,
		peerStore: peerStore,
		metricer:  metricer,
	}
}

// SnapshotHook returns a function that is called periodically by the pubsub library to inspect the peer scores.
// It is passed into the pubsub library as a [pubsub.ExtendedPeerScoreInspectFn] in the [pubsub.WithPeerScoreInspect] option.
// The returned [pubsub.ExtendedPeerScoreInspectFn] is called with a mapping of peer IDs to peer score snapshots.
func (s *scorer) SnapshotHook() pubsub.ExtendedPeerScoreInspectFn {
	// peer := s.peerStore.Get(peer.ID)
	// loop through each peer ID, get the score
	// if the score < the configured threshold, ban the peer
	// factor in the number of connections/disconnections into the score
	// e.g., score = score - (s.peerConnections[peerID] * ConnectionFactor)
	// s.connGater.BanAddr(peerID)

	return func(m map[peer.ID]*pubsub.PeerScoreSnapshot) {
		for id, snap := range m {
			// Record peer score in the metricer
			s.metricer.RecordPeerScoring(id, snap.Score)

			// TODO: encorporate the number of peer connections/disconnections into the score
			// TODO: or should we just affect the score in the OnConnect/OnDisconnect methods?
			// TODO: if we don't have to do this calculation here, we can push score updates to the metricer
			// TODO: which would leave the scoring to the pubsub lib
			// peer, err := s.peerStore.Get(id)
			// if err != nil {
			// }

			// Check if the peer score is below the threshold
			// If so, we need to block the peer
			if snap.Score < PeerScoreThreshold {
				_ = s.connGater.BlockPeer(id)
			}
			// Unblock peers whose score has recovered to an acceptable level
			if (snap.Score > PeerScoreThreshold) && contains(s.connGater.ListBlockedPeers(), id) {
				_ = s.connGater.UnblockPeer(id)
			}
		}
	}
}

func contains(peers []peer.ID, id peer.ID) bool {
	for _, v := range peers {
		if v == id {
			return true
		}
	}

	return false
}

// call the two methods below from the notifier

// OnConnect is called when a peer connects.
// See [p2p.NotificationsMetricer] for invocation.
func (s *scorer) OnConnect() {
	// record a connection
}

// OnDisconnect is called when a peer disconnects.
// See [p2p.NotificationsMetricer] for invocation.
func (s *scorer) OnDisconnect() {
	// record a disconnection
}
