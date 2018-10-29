// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package test

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

// StateChangeType is the type of state change request.
type StateChangeType uint8

var (
	// ErrDuplicatedChange means the change request is already applied.
	ErrDuplicatedChange = errors.New("duplicated change")
	// ErrForkedCRS means a different CRS for one round is proposed.
	ErrForkedCRS = errors.New("forked CRS")
	// ErrMissingPreviousCRS means previous CRS not found when
	// proposing a specific round of CRS.
	ErrMissingPreviousCRS = errors.New("missing previous CRS")
	// ErrUnknownStateChangeType means a StateChangeType is not recognized.
	ErrUnknownStateChangeType = errors.New("unknown state change type")
	// ErrProposerIsFinal means a proposer of one complaint is finalized.
	ErrProposerIsFinal = errors.New("proposer is final")
	// ErrStateConfigNotEqual means configuration part of two states is not
	// equal.
	ErrStateConfigNotEqual = errors.New("config not equal")
	// ErrStateLocalFlagNotEqual means local flag of two states is not equal.
	ErrStateLocalFlagNotEqual = errors.New("local flag not equal")
	// ErrStateNodeSetNotEqual means node sets of two states are not equal.
	ErrStateNodeSetNotEqual = errors.New("node set not equal")
	// ErrStateDKGComplaintsNotEqual means DKG complaints for two states are not
	// equal.
	ErrStateDKGComplaintsNotEqual = errors.New("dkg complaints not equal")
	// ErrStateDKGMasterPublicKeysNotEqual means DKG master public keys of two
	// states are not equal.
	ErrStateDKGMasterPublicKeysNotEqual = errors.New(
		"dkg master public keys not equal")
	// ErrStateDKGFinalsNotEqual means DKG finalizations of two states are not
	// equal.
	ErrStateDKGFinalsNotEqual = errors.New("dkg finalizations not equal")
	// ErrStateCRSsNotEqual means CRSs of two states are not equal.
	ErrStateCRSsNotEqual = errors.New("crs not equal")
	// ErrStatePendingChangesNotEqual means pending change requests of two
	// states are not equal.
	ErrStatePendingChangesNotEqual = errors.New("pending changes not equal")
)

// Types of state change.
const (
	StateChangeNothing StateChangeType = iota
	// DKG & CRS
	StateAddCRS
	StateAddDKGComplaint
	StateAddDKGMasterPublicKey
	StateAddDKGFinal
	// Configuration related.
	StateChangeNumChains
	StateChangeLambdaBA
	StateChangeLambdaDKG
	StateChangeRoundInterval
	StateChangeMinBlockInterval
	StateChangeMaxBlockInterval
	StateChangeK
	StateChangePhiRatio
	StateChangeNotarySetSize
	StateChangeDKGSetSize
	// Node set related.
	StateAddNode
)

type crsAdditionRequest struct {
	Round uint64      `json:"round"`
	CRS   common.Hash `json:"crs"`
}

// StateChangeRequest carries information of state change request.
type StateChangeRequest struct {
	Type    StateChangeType `json:"type"`
	Payload interface{}     `json:"payload"`
}

type rawStateChangeRequest struct {
	Type    StateChangeType
	Payload rlp.RawValue
}

// State emulates what the global state in governace contract on a fullnode.
type State struct {
	// Configuration related.
	numChains        uint32
	lambdaBA         time.Duration
	lambdaDKG        time.Duration
	k                int
	phiRatio         float32
	notarySetSize    uint32
	dkgSetSize       uint32
	roundInterval    time.Duration
	minBlockInterval time.Duration
	maxBlockInterval time.Duration
	// Nodes
	nodes map[types.NodeID]crypto.PublicKey
	// DKG & CRS
	dkgComplaints       map[uint64]map[types.NodeID][]*typesDKG.Complaint
	dkgMasterPublicKeys map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey
	dkgFinals           map[uint64]map[types.NodeID]*typesDKG.Finalize
	crs                 []common.Hash
	// Other stuffs
	local bool
	lock  sync.RWMutex
	// ChangeRequest(s) are organized as map, indexed by type of state change.
	// For each time to apply state change, only the last request would be
	// applied.
	pendingChangedConfigs      map[StateChangeType]interface{}
	pendingNodes               [][]byte
	pendingDKGComplaints       []*typesDKG.Complaint
	pendingDKGFinals           []*typesDKG.Finalize
	pendingDKGMasterPublicKeys []*typesDKG.MasterPublicKey
	pendingCRS                 []*crsAdditionRequest
	pendingChangesLock         sync.Mutex
}

// NewState constructs an State instance with genesis information, including:
//  - node set
//  - crs
func NewState(
	nodePubKeys []crypto.PublicKey, lambda time.Duration, local bool) *State {
	nodes := make(map[types.NodeID]crypto.PublicKey)
	for _, key := range nodePubKeys {
		nodes[types.NewNodeID(key)] = key
	}
	genesisCRS := crypto.Keccak256Hash([]byte("__ DEXON"))
	return &State{
		local:                 local,
		numChains:             uint32(len(nodes)),
		lambdaBA:              lambda,
		lambdaDKG:             lambda * 10,
		roundInterval:         lambda * 10000,
		minBlockInterval:      time.Millisecond * 1,
		maxBlockInterval:      lambda * 8,
		crs:                   []common.Hash{genesisCRS},
		nodes:                 nodes,
		phiRatio:              0.667,
		k:                     0,
		notarySetSize:         uint32(len(nodes)),
		dkgSetSize:            uint32(len(nodes)),
		pendingChangedConfigs: make(map[StateChangeType]interface{}),
		dkgFinals: make(
			map[uint64]map[types.NodeID]*typesDKG.Finalize),
		dkgComplaints: make(
			map[uint64]map[types.NodeID][]*typesDKG.Complaint),
		dkgMasterPublicKeys: make(
			map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey),
	}
}

// Snapshot returns configration that could be snapshotted.
func (s *State) Snapshot() (*types.Config, []crypto.PublicKey) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	// Clone a node set.
	nodes := make([]crypto.PublicKey, 0, len(s.nodes))
	for _, key := range s.nodes {
		nodes = append(nodes, key)
	}
	return &types.Config{
		NumChains:        s.numChains,
		LambdaBA:         s.lambdaBA,
		LambdaDKG:        s.lambdaDKG,
		K:                s.k,
		PhiRatio:         s.phiRatio,
		NotarySetSize:    s.notarySetSize,
		DKGSetSize:       s.dkgSetSize,
		RoundInterval:    s.roundInterval,
		MinBlockInterval: s.minBlockInterval,
		MaxBlockInterval: s.maxBlockInterval,
	}, nodes
}

func (s *State) unpackPayload(
	raw *rawStateChangeRequest) (v interface{}, err error) {
	switch raw.Type {
	case StateAddCRS:
		v = &crsAdditionRequest{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGComplaint:
		v = &typesDKG.Complaint{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGMasterPublicKey:
		v = &typesDKG.MasterPublicKey{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGFinal:
		v = &typesDKG.Finalize{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateChangeNumChains:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeLambdaBA:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeLambdaDKG:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeRoundInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeMinBlockInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeMaxBlockInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeK:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangePhiRatio:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeNotarySetSize:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeDKGSetSize:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateAddNode:
		var tmp []byte
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	default:
		err = ErrUnknownStateChangeType
	}
	if err != nil {
		return
	}
	return
}

func (s *State) cloneDKGComplaint(
	comp *typesDKG.Complaint) (copied *typesDKG.Complaint) {
	b, err := rlp.EncodeToBytes(comp)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.Complaint{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

func (s *State) cloneDKGMasterPublicKey(mpk *typesDKG.MasterPublicKey) (
	copied *typesDKG.MasterPublicKey) {
	b, err := rlp.EncodeToBytes(mpk)
	if err != nil {
		panic(err)
	}
	copied = typesDKG.NewMasterPublicKey()
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

func (s *State) cloneDKGFinalize(final *typesDKG.Finalize) (
	copied *typesDKG.Finalize) {
	b, err := rlp.EncodeToBytes(final)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.Finalize{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

// Equal checks equality between State instance.
func (s *State) Equal(other *State) error {
	// Check configuration part.
	configEqual := s.numChains == other.numChains &&
		s.lambdaBA == other.lambdaBA &&
		s.lambdaDKG == other.lambdaDKG &&
		s.k == other.k &&
		s.phiRatio == other.phiRatio &&
		s.notarySetSize == other.notarySetSize &&
		s.dkgSetSize == other.dkgSetSize &&
		s.roundInterval == other.roundInterval &&
		s.minBlockInterval == other.minBlockInterval &&
		s.maxBlockInterval == other.maxBlockInterval
	if !configEqual {
		return ErrStateConfigNotEqual
	}
	// Check local flag.
	if s.local != other.local {
		return ErrStateLocalFlagNotEqual
	}
	// Check node set.
	if len(s.nodes) != len(other.nodes) {
		return ErrStateNodeSetNotEqual
	}
	for nID, key := range s.nodes {
		otherKey, exists := other.nodes[nID]
		if !exists {
			return ErrStateNodeSetNotEqual
		}
		if bytes.Compare(key.Bytes(), otherKey.Bytes()) != 0 {
			return ErrStateNodeSetNotEqual
		}
	}
	// Check DKG Complaints, here I assume the addition sequence of complaints
	// proposed by one node would be identical on each node (this should be true
	// when state change requests are carried by blocks and executed in order).
	if len(s.dkgComplaints) != len(other.dkgComplaints) {
		return ErrStateDKGComplaintsNotEqual
	}
	for round, compsForRound := range s.dkgComplaints {
		otherCompsForRound, exists := other.dkgComplaints[round]
		if !exists {
			return ErrStateDKGComplaintsNotEqual
		}
		if len(compsForRound) != len(otherCompsForRound) {
			return ErrStateDKGComplaintsNotEqual
		}
		for nID, comps := range compsForRound {
			otherComps, exists := otherCompsForRound[nID]
			if !exists {
				return ErrStateDKGComplaintsNotEqual
			}
			if len(comps) != len(otherComps) {
				return ErrStateDKGComplaintsNotEqual
			}
			for idx, comp := range comps {
				if !comp.Equal(otherComps[idx]) {
					return ErrStateDKGComplaintsNotEqual
				}
			}
		}
	}
	// Check DKG master public keys.
	if len(s.dkgMasterPublicKeys) != len(other.dkgMasterPublicKeys) {
		return ErrStateDKGMasterPublicKeysNotEqual
	}
	for round, mKeysForRound := range s.dkgMasterPublicKeys {
		otherMKeysForRound, exists := other.dkgMasterPublicKeys[round]
		if !exists {
			return ErrStateDKGMasterPublicKeysNotEqual
		}
		if len(mKeysForRound) != len(otherMKeysForRound) {
			return ErrStateDKGMasterPublicKeysNotEqual
		}
		for nID, mKey := range mKeysForRound {
			otherMKey, exists := otherMKeysForRound[nID]
			if !exists {
				return ErrStateDKGMasterPublicKeysNotEqual
			}
			if !mKey.Equal(otherMKey) {
				return ErrStateDKGMasterPublicKeysNotEqual
			}
		}
	}
	// Check DKG finals.
	if len(s.dkgFinals) != len(other.dkgFinals) {
		return ErrStateDKGFinalsNotEqual
	}
	for round, finalsForRound := range s.dkgFinals {
		otherFinalsForRound, exists := other.dkgFinals[round]
		if !exists {
			return ErrStateDKGFinalsNotEqual
		}
		if len(finalsForRound) != len(otherFinalsForRound) {
			return ErrStateDKGFinalsNotEqual
		}
		for nID, final := range finalsForRound {
			otherFinal, exists := otherFinalsForRound[nID]
			if !exists {
				return ErrStateDKGFinalsNotEqual
			}
			if !final.Equal(otherFinal) {
				return ErrStateDKGFinalsNotEqual
			}
		}
	}
	// Check CRS part.
	if len(s.crs) != len(other.crs) {
		return ErrStateCRSsNotEqual
	}
	for idx, crs := range s.crs {
		if crs != other.crs[idx] {
			return ErrStateCRSsNotEqual
		}
	}
	// Check pending changes.
	if !reflect.DeepEqual(
		s.pendingChangedConfigs, other.pendingChangedConfigs) {
		return ErrStatePendingChangesNotEqual
	}
	if !reflect.DeepEqual(s.pendingCRS, other.pendingCRS) {
		return ErrStatePendingChangesNotEqual
	}
	if !reflect.DeepEqual(s.pendingNodes, other.pendingNodes) {
		return ErrStatePendingChangesNotEqual
	}
	// Check pending DKG complaints.
	if len(s.pendingDKGComplaints) != len(other.pendingDKGComplaints) {
		return ErrStatePendingChangesNotEqual
	}
	for idx, comp := range s.pendingDKGComplaints {
		if !comp.Equal(other.pendingDKGComplaints[idx]) {
			return ErrStatePendingChangesNotEqual
		}
	}
	// Check pending DKG finals.
	if len(s.pendingDKGFinals) != len(other.pendingDKGFinals) {
		return ErrStatePendingChangesNotEqual
	}
	for idx, final := range s.pendingDKGFinals {
		if !final.Equal(other.pendingDKGFinals[idx]) {
			return ErrStatePendingChangesNotEqual
		}
	}
	// Check pending DKG Master public keys.
	if len(s.pendingDKGMasterPublicKeys) !=
		len(other.pendingDKGMasterPublicKeys) {
		return ErrStatePendingChangesNotEqual
	}
	for idx, mKey := range s.pendingDKGMasterPublicKeys {
		if !mKey.Equal(other.pendingDKGMasterPublicKeys[idx]) {
			return ErrStatePendingChangesNotEqual
		}
	}
	return nil
}

// Clone returns a copied State instance.
func (s *State) Clone() (copied *State) {
	// Clone configuration parts.
	copied = &State{
		numChains:        s.numChains,
		lambdaBA:         s.lambdaBA,
		lambdaDKG:        s.lambdaDKG,
		k:                s.k,
		phiRatio:         s.phiRatio,
		notarySetSize:    s.notarySetSize,
		dkgSetSize:       s.dkgSetSize,
		roundInterval:    s.roundInterval,
		minBlockInterval: s.minBlockInterval,
		maxBlockInterval: s.maxBlockInterval,
		local:            s.local,
		nodes:            make(map[types.NodeID]crypto.PublicKey),
		dkgComplaints: make(
			map[uint64]map[types.NodeID][]*typesDKG.Complaint),
		dkgMasterPublicKeys: make(
			map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey),
		dkgFinals:             make(map[uint64]map[types.NodeID]*typesDKG.Finalize),
		pendingChangedConfigs: make(map[StateChangeType]interface{}),
	}
	// Nodes
	for nID, key := range s.nodes {
		copied.nodes[nID] = key
	}
	// DKG & CRS
	for round, complaintsForRound := range s.dkgComplaints {
		copied.dkgComplaints[round] =
			make(map[types.NodeID][]*typesDKG.Complaint)
		for nID, comps := range complaintsForRound {
			tmpComps := []*typesDKG.Complaint{}
			for _, comp := range comps {
				tmpComps = append(tmpComps, s.cloneDKGComplaint(comp))
			}
			copied.dkgComplaints[round][nID] = tmpComps
		}
	}
	for round, mKeysForRound := range s.dkgMasterPublicKeys {
		copied.dkgMasterPublicKeys[round] =
			make(map[types.NodeID]*typesDKG.MasterPublicKey)
		for nID, mKey := range mKeysForRound {
			copied.dkgMasterPublicKeys[round][nID] =
				s.cloneDKGMasterPublicKey(mKey)
		}
	}
	for round, finalsForRound := range s.dkgFinals {
		copied.dkgFinals[round] = make(map[types.NodeID]*typesDKG.Finalize)
		for nID, final := range finalsForRound {
			copied.dkgFinals[round][nID] = s.cloneDKGFinalize(final)
		}
	}
	for _, crs := range s.crs {
		copied.crs = append(copied.crs, crs)
	}
	// Pending Changes
	for t, v := range s.pendingChangedConfigs {
		copied.pendingChangedConfigs[t] = v
	}
	for _, bs := range s.pendingNodes {
		tmpBytes := make([]byte, len(bs))
		copy(tmpBytes, bs)
		copied.pendingNodes = append(copied.pendingNodes, tmpBytes)
	}
	for _, comp := range s.pendingDKGComplaints {
		copied.pendingDKGComplaints = append(
			copied.pendingDKGComplaints, s.cloneDKGComplaint(comp))
	}
	for _, final := range s.pendingDKGFinals {
		copied.pendingDKGFinals = append(
			copied.pendingDKGFinals, s.cloneDKGFinalize(final))
	}
	for _, mKey := range s.pendingDKGMasterPublicKeys {
		copied.pendingDKGMasterPublicKeys = append(
			copied.pendingDKGMasterPublicKeys, s.cloneDKGMasterPublicKey(mKey))
	}
	for _, req := range s.pendingCRS {
		copied.pendingCRS = append(copied.pendingCRS, &crsAdditionRequest{
			Round: req.Round,
			CRS:   req.CRS,
		})
	}
	return
}

// Apply change requests, this function would also
// be called when we extract these request from delivered blocks.
func (s *State) Apply(reqsAsBytes []byte) (err error) {
	// Try to unmarshal this byte stream into []*StateChangeRequest.
	rawReqs := []*rawStateChangeRequest{}
	if err = rlp.DecodeBytes(reqsAsBytes, &rawReqs); err != nil {
		return
	}
	var reqs []*StateChangeRequest
	for _, r := range rawReqs {
		var payload interface{}
		if payload, err = s.unpackPayload(r); err != nil {
			return
		}
		reqs = append(reqs, &StateChangeRequest{
			Type:    r.Type,
			Payload: payload,
		})
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, req := range reqs {
		if err = s.applyRequest(req); err != nil {
			return
		}
	}
	return
}

// PackRequests pack current pending requests as byte slice, which
// could be sent as blocks' payload and unmarshall back to apply.
func (s *State) PackRequests() (b []byte, err error) {
	packed := []*StateChangeRequest{}
	s.pendingChangesLock.Lock()
	defer s.pendingChangesLock.Unlock()
	// Pack simple configuration changes first. There should be no
	// validity problems for those changes.
	for k, v := range s.pendingChangedConfigs {
		packed = append(packed, &StateChangeRequest{
			Type:    k,
			Payload: v,
		})
	}
	s.pendingChangedConfigs = make(map[StateChangeType]interface{})
	// For other changes, we need to check their validity.
	s.lock.RLock()
	defer s.lock.RUnlock()
	for _, bytesOfKey := range s.pendingNodes {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddNode,
			Payload: bytesOfKey,
		})
	}
	for _, comp := range s.pendingDKGComplaints {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGComplaint,
			Payload: comp,
		})
	}
	for _, final := range s.pendingDKGFinals {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGFinal,
			Payload: final,
		})
	}
	for _, masterPubKey := range s.pendingDKGMasterPublicKeys {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGMasterPublicKey,
			Payload: masterPubKey,
		})
	}
	for _, crs := range s.pendingCRS {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddCRS,
			Payload: crs,
		})
	}
	if b, err = rlp.EncodeToBytes(packed); err != nil {
		return
	}
	return
}

// isValidRequest checks if this request is valid to proceed or not.
func (s *State) isValidRequest(req *StateChangeRequest) (err error) {
	// NOTE: there would be no lock in this helper, callers should be
	//       responsible for acquiring appropriate lock.
	switch req.Type {
	case StateAddDKGComplaint:
		comp := req.Payload.(*typesDKG.Complaint)
		// If we've received DKG final from that proposer, we would ignore
		// its complaint.
		if _, exists := s.dkgFinals[comp.Round][comp.ProposerID]; exists {
			return ErrProposerIsFinal
		}
		// If we've received identical complaint, ignore it.
		compForRound, exists := s.dkgComplaints[comp.Round]
		if !exists {
			break
		}
		comps, exists := compForRound[comp.ProposerID]
		if !exists {
			break
		}
		for _, tmpComp := range comps {
			if tmpComp == comp {
				return ErrDuplicatedChange
			}
		}
	case StateAddCRS:
		crsReq := req.Payload.(*crsAdditionRequest)
		if uint64(len(s.crs)) > crsReq.Round {
			if !s.crs[crsReq.Round].Equal(crsReq.CRS) {
				return ErrForkedCRS
			}
			return ErrDuplicatedChange
		} else if uint64(len(s.crs)) == crsReq.Round {
			return nil
		} else {
			return ErrMissingPreviousCRS
		}
	}
	return nil
}

// applyRequest applies a single StateChangeRequest.
func (s *State) applyRequest(req *StateChangeRequest) error {
	// NOTE: there would be no lock in this helper, callers should be
	//       responsible for acquiring appropriate lock.
	switch req.Type {
	case StateAddNode:
		pubKey, err := ecdsa.NewPublicKeyFromByteSlice(req.Payload.([]byte))
		if err != nil {
			return err
		}
		s.nodes[types.NewNodeID(pubKey)] = pubKey
	case StateAddCRS:
		crsRequest := req.Payload.(*crsAdditionRequest)
		if crsRequest.Round != uint64(len(s.crs)) {
			return ErrDuplicatedChange
		}
		s.crs = append(s.crs, crsRequest.CRS)
	case StateAddDKGComplaint:
		comp := req.Payload.(*typesDKG.Complaint)
		if _, exists := s.dkgComplaints[comp.Round]; !exists {
			s.dkgComplaints[comp.Round] = make(
				map[types.NodeID][]*typesDKG.Complaint)
		}
		s.dkgComplaints[comp.Round][comp.ProposerID] = append(
			s.dkgComplaints[comp.Round][comp.ProposerID], comp)
	case StateAddDKGMasterPublicKey:
		mKey := req.Payload.(*typesDKG.MasterPublicKey)
		if _, exists := s.dkgMasterPublicKeys[mKey.Round]; !exists {
			s.dkgMasterPublicKeys[mKey.Round] = make(
				map[types.NodeID]*typesDKG.MasterPublicKey)
		}
		s.dkgMasterPublicKeys[mKey.Round][mKey.ProposerID] = mKey
	case StateAddDKGFinal:
		final := req.Payload.(*typesDKG.Finalize)
		if _, exists := s.dkgFinals[final.Round]; !exists {
			s.dkgFinals[final.Round] = make(map[types.NodeID]*typesDKG.Finalize)
		}
		s.dkgFinals[final.Round][final.ProposerID] = final
	case StateChangeNumChains:
		s.numChains = req.Payload.(uint32)
	case StateChangeLambdaBA:
		s.lambdaBA = time.Duration(req.Payload.(uint64))
	case StateChangeLambdaDKG:
		s.lambdaDKG = time.Duration(req.Payload.(uint64))
	case StateChangeRoundInterval:
		s.roundInterval = time.Duration(req.Payload.(uint64))
	case StateChangeMinBlockInterval:
		s.minBlockInterval = time.Duration(req.Payload.(uint64))
	case StateChangeMaxBlockInterval:
		s.maxBlockInterval = time.Duration(req.Payload.(uint64))
	case StateChangeK:
		s.k = int(req.Payload.(uint64))
	case StateChangePhiRatio:
		s.phiRatio = math.Float32frombits(req.Payload.(uint32))
	case StateChangeNotarySetSize:
		s.notarySetSize = req.Payload.(uint32)
	case StateChangeDKGSetSize:
		s.dkgSetSize = req.Payload.(uint32)
	default:
		return errors.New("you are definitely kidding me")
	}
	return nil
}

// ProposeCRS propose a new CRS for a specific round.
func (s *State) ProposeCRS(round uint64, crs common.Hash) (err error) {
	err = s.RequestChange(StateAddCRS, &crsAdditionRequest{
		Round: round,
		CRS:   crs,
	})
	return
}

// RequestChange submits a state change request.
func (s *State) RequestChange(
	t StateChangeType, payload interface{}) (err error) {
	// Patch input parameter's type.
	switch t {
	case StateAddNode:
		payload = payload.(crypto.PublicKey).Bytes()
	case StateChangeLambdaBA,
		StateChangeLambdaDKG,
		StateChangeRoundInterval,
		StateChangeMinBlockInterval,
		StateChangeMaxBlockInterval:
		payload = uint64(payload.(time.Duration))
	case StateChangeK:
		payload = uint64(payload.(int))
	case StateChangePhiRatio:
		payload = math.Float32bits(payload.(float32))
	}
	req := &StateChangeRequest{
		Type:    t,
		Payload: payload,
	}
	if s.local {
		err = func() error {
			s.lock.Lock()
			defer s.lock.Unlock()
			if err := s.isValidRequest(req); err != nil {
				return err
			}
			return s.applyRequest(req)
		}()
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err = s.isValidRequest(req); err != nil {
		return
	}
	s.pendingChangesLock.Lock()
	defer s.pendingChangesLock.Unlock()
	switch t {
	case StateAddNode:
		s.pendingNodes = append(s.pendingNodes, payload.([]byte))
	case StateAddCRS:
		s.pendingCRS = append(s.pendingCRS, payload.(*crsAdditionRequest))
	case StateAddDKGComplaint:
		s.pendingDKGComplaints = append(
			s.pendingDKGComplaints, payload.(*typesDKG.Complaint))
	case StateAddDKGMasterPublicKey:
		s.pendingDKGMasterPublicKeys = append(
			s.pendingDKGMasterPublicKeys, payload.(*typesDKG.MasterPublicKey))
	case StateAddDKGFinal:
		s.pendingDKGFinals = append(
			s.pendingDKGFinals, payload.(*typesDKG.Finalize))
	default:
		s.pendingChangedConfigs[t] = payload
	}
	return
}

// CRS access crs proposed for that round.
func (s *State) CRS(round uint64) common.Hash {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if round >= uint64(len(s.crs)) {
		return common.Hash{}
	}
	return s.crs[round]
}

// DKGComplaints access current received dkg complaints for that round.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) DKGComplaints(round uint64) []*typesDKG.Complaint {
	s.lock.RLock()
	defer s.lock.RUnlock()
	comps, exists := s.dkgComplaints[round]
	if !exists {
		return nil
	}
	tmpComps := make([]*typesDKG.Complaint, 0, len(comps))
	for _, compProp := range comps {
		for _, comp := range compProp {
			tmpComps = append(tmpComps, s.cloneDKGComplaint(comp))
		}
	}
	return tmpComps
}

// DKGMasterPublicKeys access current received dkg master public keys for that
// round. This information won't be snapshot, thus can't be cached in
// test.Governance.
func (s *State) DKGMasterPublicKeys(round uint64) []*typesDKG.MasterPublicKey {
	s.lock.RLock()
	defer s.lock.RUnlock()
	masterPublicKeys, exists := s.dkgMasterPublicKeys[round]
	if !exists {
		return nil
	}
	mpks := make([]*typesDKG.MasterPublicKey, 0, len(masterPublicKeys))
	for _, mpk := range masterPublicKeys {
		mpks = append(mpks, s.cloneDKGMasterPublicKey(mpk))
	}
	return mpks
}

// IsDKGFinal checks if current received dkg finals exceeds threshold.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) IsDKGFinal(round uint64, threshold int) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.dkgFinals[round]) > threshold
}