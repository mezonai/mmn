#!/usr/bin/env node

// Simple Node.js WebSocket client for testing MMN events
const WebSocket = require('ws');

const ws = new WebSocket('ws://localhost:8090/ws');

ws.on('open', function open() {
    console.log('🟢 Connected to MMN WebSocket');
    console.log('📡 Listening for events...\n');
});

ws.on('message', function message(data) {
    try {
        const event = JSON.parse(data);
        const timestamp = new Date(event.timestamp * 1000).toLocaleTimeString();
        
        console.log(`📧 [${timestamp}] ${event.type.toUpperCase()}`);
        
        switch (event.type) {
            case 'tx_submitted':
                console.log(`   Hash: ${event.data.tx_hash}`);
                console.log(`   From: ${event.data.tx_data?.sender || 'N/A'}`);
                console.log(`   To: ${event.data.tx_data?.recipient || 'N/A'}`);
                console.log(`   Amount: ${event.data.tx_data?.amount || 0}`);
                break;
                
            case 'tx_confirmed':
                console.log(`   Hash: ${event.data.tx_hash}`);
                console.log(`   Slot: ${event.data.slot}`);
                console.log(`   Amount: ${event.data.tx_data?.amount || 0}`);
                break;
                
            case 'tx_failed':
                console.log(`   Hash: ${event.data.tx_hash}`);
                console.log(`   Error: ${event.data.error}`);
                break;
                
            case 'block_created':
                console.log(`   Slot: ${event.data.slot}`);
                console.log(`   Transactions: ${event.data.tx_count}`);
                break;
                
            case 'mempool_stats':
                console.log(`   Transaction Count: ${event.data.tx_count}`);
                console.log(`   Max Size: ${event.data.max_size}`);
                console.log(`   Full: ${event.data.is_full ? 'Yes' : 'No'}`);
                break;
        }
        console.log('');
        
    } catch (error) {
        console.error('❌ Error parsing event:', error);
    }
});

ws.on('close', function close() {
    console.log('🔴 Disconnected from MMN WebSocket');
});

ws.on('error', function error(err) {
    console.error('❌ WebSocket error:', err.message);
});

// Keep process alive
process.on('SIGINT', () => {
    console.log('\n👋 Closing WebSocket connection...');
    ws.close();
    process.exit(0);
});

console.log('🚀 MMN WebSocket Client');
console.log('🔗 Connecting to ws://localhost:8090/ws');
console.log('🛑 Press Ctrl+C to stop\n');
