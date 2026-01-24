package internal

import (
	math_rand "math/rand"
	"sync"
)

// TODO:
// For now, recipients will be usernames, but it really should be connectionIds (userDevices aka. destinations).
// The test-client could actually know of the connectionIds: see WSGW_ACK_NEW_CONN_WITH_CONN_ID (AckNewConnWithConnId)
type deliveryTracker struct {
	pendingDeliveries map[string]*message
	lock              sync.RWMutex
	notifyEmpty       func()
}

func newDeliveryTracker() *deliveryTracker {
	return &deliveryTracker{
		pendingDeliveries: map[string]*message{},
		lock:              sync.RWMutex{},
	}
}

func (mr *deliveryTracker) get(msgId string) *message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	msg, has := mr.pendingDeliveries[msgId]

	if !has {
		return nil
	}

	return msg
}

// Returns the message, if it was delivered to all recipients
func (mr *deliveryTracker) markDelivered(msgId string, recipient recipientName) *message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	defer func() {
		if mr.notifyEmpty != nil && len(mr.pendingDeliveries) == 0 {
			mr.notifyEmpty()
		}
	}()

	msg, has := mr.pendingDeliveries[msgId]
	if !has {
		return nil
	}

	updatedRecipList := []recipientName{}
	for _, recip := range msg.recipients {
		if recip == recipient {
			continue
		}
		updatedRecipList = append(updatedRecipList, recip)
	}

	if len(updatedRecipList) == 0 {
		delete(mr.pendingDeliveries, msgId)
		return msg
	}

	msg.recipients = updatedRecipList
	mr.pendingDeliveries[msgId] = msg

	return nil
}

func (mr *deliveryTracker) watchDraining(notifyEmpty func()) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	if len(mr.pendingDeliveries) == 0 {
		notifyEmpty()
	}

	mr.notifyEmpty = notifyEmpty
}

func selectRecipients(candidates []string, howMany int) []string {
	n := len(candidates)
	if howMany >= n {
		return candidates
	}

	result := make([]string, 0, howMany)
	k := howMany // elements still needed

	for i := range n {
		remaining := n - i // elements left to visit (including current)

		// Probability of selecting current element = k / remaining
		if math_rand.Intn(remaining) < k {
			result = append(result, candidates[i])
			k-- // one less element needed
			if k == 0 {
				break // done selecting
			}
		}
	}

	return result
}
