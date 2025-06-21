package conntrack

import (
	"slices"
	"sync"
)

// TODO: persist this data in a globally accessible store to support clustered applications
type UserWsConnectionMap struct {
	m  map[string][]string
	mx sync.Mutex
}

func (connmap *UserWsConnectionMap) AddConnection(userId string, connId string) {
	connmap.mx.Lock()
	defer connmap.mx.Unlock()

	value, exists := connmap.m[userId]

	if !exists {
		connmap.m[userId] = []string{connId}
	} else {
		connmap.m[userId] = append(value, connId)
	}
}

func (connmap *UserWsConnectionMap) RemoveConnection(userId string, connId string) bool {
	connmap.mx.Lock()
	defer connmap.mx.Unlock()

	connIdList, exists := connmap.m[userId]

	if exists {
		connIdList = slices.DeleteFunc(connIdList, func(elem string) bool {
			return elem == connId
		})
		connmap.m[userId] = connIdList
	}

	return exists
}

func (connmap *UserWsConnectionMap) GetConnections(userId string) []string {
	connmap.mx.Lock()
	defer connmap.mx.Unlock()

	connIdList, exists := connmap.m[userId]

	if exists {
		tmp := make([]string, len(connIdList))
		copy(tmp, connIdList)
		return tmp
	}

	return nil
}

func NewUserWsConnectionMap() UserWsConnectionMap {
	return UserWsConnectionMap{
		m: map[string][]string{},
	}
}
