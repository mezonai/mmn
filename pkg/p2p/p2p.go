// File: pkg/p2p/p2p.go
package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"cryptocypher/pkg/blockchain"
)

// Message defines the structure for P2P messages.
type Message struct {
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Node represents a peer in the network.
type Node struct {
	Address    string                 // Address to listen on (e.g. "localhost:8000")
	Peers      []string               // List of known peer addresses
	Blockchain *blockchain.Blockchain // Pointer to our blockchain
}

// NewNode initializes a new node.
func NewNode(address string, peers []string, bc *blockchain.Blockchain) *Node {
	return &Node{
		Address:    address,
		Peers:      peers,
		Blockchain: bc,
	}
}

// Start launches the TCP server to listen for incoming connections.
func (n *Node) Start() {
	ln, err := net.Listen("tcp", n.Address)
	if err != nil {
		fmt.Println("Error starting P2P server:", err)
		return
	}
	defer ln.Close()

	fmt.Println("P2P node listening on", n.Address)
	// Start periodic peer discovery.
	go n.periodicPeerDiscovery()
	go n.connectToPeers() // Initiate outgoing connections to known peers

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		go n.handleConnection(conn)
	}
}

// periodicPeerDiscovery periodically requests peer lists from known peers.
func (n *Node) periodicPeerDiscovery() {
	for {
		time.Sleep(30 * time.Second) // Adjust interval as needed.
		n.broadcastGetPeers()
	}
}

// broadcastGetPeers sends a GET_PEERS command to all known peers.
func (n *Node) broadcastGetPeers() {
	msg := Message{Command: "GET_PEERS"}
	for _, addr := range n.Peers {
		go func(peerAddr string) {
			conn, err := net.Dial("tcp", peerAddr)
			if err != nil {
				// Could not connect; skip.
				return
			}
			defer conn.Close()
			n.sendMessage(conn, msg)
		}(addr)
	}
}

// handleConnection processes an incoming connection.
func (n *Node) handleConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Println("Error unmarshalling message:", err)
			continue
		}
		n.handleMessage(msg, conn)
	}
}

// handleMessage routes the message based on its command.
func (n *Node) handleMessage(msg Message, conn net.Conn) {
	switch msg.Command {
	case "GET_CHAIN":
		n.sendChain(conn)
	case "GET_CHAIN_RESPONSE":
		n.handleChainUpdate(msg.Data)
	case "CHAIN_UPDATE":
		n.handleChainUpdate(msg.Data)
	case "NEW_BLOCK":
		n.handleNewBlock(msg.Data)
	case "HEARTBEAT":
		n.sendHeartbeatAck(conn)
	case "HEARTBEAT_ACK":
		fmt.Println("Received heartbeat acknowledgment.")
	case "GET_PEERS":
		n.handleGetPeers(conn)
	case "PEER_LIST":
		n.handlePeerList(msg.Data)
	default:
		fmt.Printf("Received unknown command: %s\n", msg.Command)
	}
}

// sendChain sends the current blockchain as a JSON blob.
func (n *Node) sendChain(conn net.Conn) {
	chainBytes, err := json.Marshal(n.Blockchain.Blocks)
	if err != nil {
		fmt.Println("Error marshalling blockchain:", err)
		return
	}
	responseMsg := Message{
		Command: "GET_CHAIN_RESPONSE",
		Data:    chainBytes,
	}
	n.sendMessage(conn, responseMsg)
}

// sendMessage writes a JSON message to a connection.
func (n *Node) sendMessage(conn net.Conn, msg Message) {
	bytes, err := json.Marshal(msg)
	if err != nil {
		fmt.Println("Error marshalling message:", err)
		return
	}
	// Append newline as a delimiter.
	conn.Write(append(bytes, '\n'))
}

// sendHeartbeatAck responds to a heartbeat with an acknowledgment.
func (n *Node) sendHeartbeatAck(conn net.Conn) {
	ack := Message{
		Command: "HEARTBEAT_ACK",
	}
	n.sendMessage(conn, ack)
}

// handleChainUpdate processes a received chain update.
func (n *Node) handleChainUpdate(data json.RawMessage) {
	var incomingChain []*blockchain.Block
	if err := json.Unmarshal(data, &incomingChain); err != nil {
		fmt.Println("Error unmarshalling chain update:", err)
		return
	}

	if blockchain.IsValidChain(incomingChain) {
		if n.Blockchain.ReplaceChain(incomingChain) {
			fmt.Println("Local chain replaced with received chain (higher cumulative difficulty).")
		} else {
			fmt.Println("Received chain valid but not stronger than the current chain.")
		}
	} else {
		fmt.Println("Received invalid chain update.")
	}
}

// handleNewBlock processes a received new block announcement.
func (n *Node) handleNewBlock(data json.RawMessage) {
	var newBlock *blockchain.Block
	if err := json.Unmarshal(data, &newBlock); err != nil {
		fmt.Println("Error unmarshalling new block:", err)
		return
	}

	lastBlock := n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1]
	if newBlock.PrevHash == lastBlock.Hash && newBlock.Hash == blockchain.CalculateHash(newBlock) {
		n.Blockchain.Blocks = append(n.Blockchain.Blocks, newBlock)
		fmt.Println("New block added to the chain.")
		n.BroadcastChainUpdate()
	} else {
		fmt.Println("Received block is invalid or does not extend the current chain.")
	}
}

// handleGetPeers responds to a GET_PEERS request by sending the current peer list.
func (n *Node) handleGetPeers(conn net.Conn) {
	// Send current peers as JSON array.
	peerListBytes, err := json.Marshal(n.Peers)
	if err != nil {
		fmt.Println("Error marshalling peer list:", err)
		return
	}
	responseMsg := Message{
		Command: "PEER_LIST",
		Data:    peerListBytes,
	}
	n.sendMessage(conn, responseMsg)
}

// handlePeerList processes a received peer list and updates the local peer list.
func (n *Node) handlePeerList(data json.RawMessage) {
	var receivedPeers []string
	if err := json.Unmarshal(data, &receivedPeers); err != nil {
		fmt.Println("Error unmarshalling peer list:", err)
		return
	}
	updated := false
	for _, peer := range receivedPeers {
		if peer != n.Address && !contains(n.Peers, peer) {
			n.Peers = append(n.Peers, peer)
			updated = true
		}
	}
	if updated {
		fmt.Println("Updated peer list:", n.Peers)
	}
}

// Utility function: checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// connectToPeers initiates connections to each known peer.
func (n *Node) connectToPeers() {
	for _, peerAddr := range n.Peers {
		go func(addr string) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				fmt.Printf("Could not connect to peer %s: %v\n", addr, err)
				return
			}
			defer conn.Close()

			// Send a GET_CHAIN message.
			msg := Message{Command: "GET_CHAIN"}
			n.sendMessage(conn, msg)

			// Also request peer list.
			getPeersMsg := Message{Command: "GET_PEERS"}
			n.sendMessage(conn, getPeersMsg)

			reader := bufio.NewReader(conn)
			responseLine, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading from peer %s: %v\n", addr, err)
				return
			}
			var respMsg Message
			if err := json.Unmarshal([]byte(responseLine), &respMsg); err != nil {
				fmt.Printf("Error unmarshalling response from peer %s: %v\n", addr, err)
				return
			}

			if respMsg.Command == "GET_CHAIN_RESPONSE" {
				n.handleChainUpdate(respMsg.Data)
			} else if respMsg.Command == "PEER_LIST" {
				n.handlePeerList(respMsg.Data)
			} else {
				fmt.Printf("Unexpected response from peer %s: %s\n", addr, respMsg.Command)
			}
		}(peerAddr)
	}
}

// BroadcastChainUpdate sends the full blockchain to all known peers as a CHAIN_UPDATE message.
func (n *Node) BroadcastChainUpdate() {
	chainBytes, err := json.Marshal(n.Blockchain.Blocks)
	if err != nil {
		fmt.Println("Error marshalling blockchain:", err)
		return
	}
	msg := Message{
		Command: "CHAIN_UPDATE",
		Data:    chainBytes,
	}
	for _, addr := range n.Peers {
		go func(peerAddr string) {
			conn, err := net.Dial("tcp", peerAddr)
			if err != nil {
				fmt.Printf("Could not connect to peer %s: %v\n", peerAddr, err)
				return
			}
			defer conn.Close()
			n.sendMessage(conn, msg)
			fmt.Printf("Broadcasted chain update to %s\n", peerAddr)
		}(addr)
	}
}
