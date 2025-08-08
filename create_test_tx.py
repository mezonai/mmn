#!/usr/bin/env python3
"""
Simple transaction creator for MMN testing
Creates a valid signed transaction for testing WebSocket events
"""

import hashlib
import ed25519
import json
import time
import sys

def create_test_transaction():
    """Create a simple test transaction with valid signature"""
    
    # Generate a test keypair (for demo - in real use, load from file)
    private_key, public_key = ed25519.create_keypair()
    
    # Create transaction data
    tx_data = {
        "type": 1,  # Faucet type for testing
        "sender": public_key.to_ascii(encoding="hex").decode(),
        "recipient": public_key.to_ascii(encoding="hex").decode(), 
        "amount": 1000,
        "timestamp": int(time.time()),
        "text_data": "test_websocket",
        "nonce": 1
    }
    
    # Create message to sign (same as Go implementation)
    message = f"{tx_data['type']}|{tx_data['sender']}|{tx_data['recipient']}|{tx_data['amount']}|{tx_data['text_data']}|{tx_data['nonce']}"
    
    # Sign the message
    signature = private_key.sign(message.encode(), encoding="hex").decode()
    tx_data["signature"] = signature
    
    return tx_data

if __name__ == "__main__":
    try:
        import ed25519
    except ImportError:
        print("Error: ed25519 library not found")
        print("Install with: pip install ed25519")
        sys.exit(1)
    
    tx = create_test_transaction()
    print(json.dumps(tx, indent=2))
