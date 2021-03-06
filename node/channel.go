// Copyright © 2014 Terry Mao, LiuDing All rights reserved.
// This file is part of gopush-cluster.

// gopush-cluster is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// gopush-cluster is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with gopush-cluster.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	log "code.google.com/p/log4go"
	"errors"
	"sync"
)

var (
	ErrMessageSave   = errors.New("Message set failed")
	ErrMessageGet    = errors.New("Message get failed")
	ErrMessageRPC    = errors.New("Message RPC not init")
	ErrAssectionConn = errors.New("Assection type Connection failed")
)

// Sequence Channel struct.
type Channel struct {
	// Mutex
	mutex *sync.Mutex
	// client conn double linked-list
	connections *Hlist
	// token
	token string
}

var messageServiceProxy = NewMessageServiceProxy()

// New a user seq stored message channel.
func NewChannel() *Channel {
	return &Channel{
		mutex:    &sync.Mutex{},
		connList: hlist.New(),
		token:    nil,
	}
}

// AddToken implements the Channel AddToken method.
func (c *Channel) AddToken(key, token string) error {
	return nil
}

// AuthToken implements the Channel AuthToken method.
func (c *Channel) AuthToken(key, token string) bool {
	return true
}

// PushMsg implements the Channel PushMsg method.
func (c *Channel) PushMsg(key string, m *Message, expire uint) error {
	var (
		oldMsg, msg, sendMsg []byte
		err                  error
	)
	c.mutex.Lock()
	// private message need persistence
	// if message expired no need persistence, only send online message
	// rewrite message id
	m.MsgId = c.timeID.ID()
	if expire > 0 {
		if err = messageServiceProxy.SavePrivateMessage(key, m.Msg, m.MsgId, expire); err != nil {
			c.mutex.Unlock()
			log.Error("messageServiceProxy.SavePrivateMessage(%s, %v, %v, %v) error(%v)", key, m.Msg, m.MsgId, expire, err)
			return err
		}
	}
	// push message
	for e := c.connections.Front(); e != nil; e = e.Next() {
		conn := e.Conn
		// if version empty then use old protocol
		if conn.Version == "" {
			if oldMsg == nil {
				oldMsg, err = m.OldBytes()
				if err != nil {
					c.mutex.Unlock()
					return err
				}
			}
			sendMsg = oldMsg
		} else {
			if msg == nil {
				msg, err = m.Bytes()
				if err != nil {
					c.mutex.Unlock()
					return err
				}
			}
			sendMsg = msg
		}
		conn.Write(key, sendMsg)
	}
	c.mutex.Unlock()
	return nil
}

// AddConn implements the Channel AddConn method.
func (c *Channel) AddConn(key string, conn *Connection) (*hlist.Node, error) {
	c.mutex.Lock()
	if c.connections.Len()+1 > Conf.MaxSubscriberPerChannel {
		c.mutex.Unlock()
		log.Error("user_key:\"%s\" exceed conn", key)
		return nil, ErrMaxConn
	}
	// send first heartbeat to tell client service is ready for accept heartbeat
	if _, err := conn.Conn.Write(HeartbeatReply); err != nil {
		c.mutex.Unlock()
		log.Error("user_key:\"%s\" write first heartbeat to client error(%v)", key, err)
		return nil, err
	}
	// add conn
	conn.Msgs = make(chan []byte, Conf.MsgBufNum)
	conn.HandleWrite(key)
	node := c.connections.PushFront(conn)
	c.mutex.Unlock()
	ConnStat.IncrAdd()
	log.Info("user_key:\"%s\" add conn = %d", key, c.connections.Len())
	return node, nil
}

// RemoveConn implements the Channel RemoveConn method.
func (c *Channel) RemoveConn(key string, node *hlist.Node) error {
	c.mutex.Lock()
	tmp := c.connections.Remove(node)
	c.mutex.Unlock()
	conn, ok := tmp.(*Connection)
	if !ok {
		return ErrAssectionConn
	}
	close(conn.Msgs)
	ConnStat.IncrRemove()
	log.Info("user_key:\"%s\" remove conn = %d", key, c.connections.Len())
	return nil
}

// Close implements the Channel Close method.
func (c *Channel) Close() error {
	c.mutex.Lock()
	for node := c.connections.Front(); node != nil; node = node.Next() {
		if err := node.Conn.Close(); err != nil {
			log.Warn("conn.Close() error(%v)", err) // ignore close error
		}
	}
	c.mutex.Unlock()
	return nil
}
