#!/usr/bin/env python3
"""
MMN WebSocket Client Example
Kết nối tới MMN WebSocket server và lắng nghe events real-time
"""

import asyncio
import websockets
import json
from datetime import datetime
import argparse
import sys

class MMNWebSocketClient:
    def __init__(self, uri="ws://localhost:8090/ws"):
        self.uri = uri
        self.websocket = None
        self.running = False
        self.stats = {
            'tx_submitted': 0,
            'tx_confirmed': 0,
            'tx_failed': 0,
            'block_created': 0,
            'mempool_stats': 0,
            'total_events': 0
        }

    async def connect(self):
        """Kết nối tới WebSocket server"""
        try:
            self.websocket = await websockets.connect(self.uri)
            print(f"🟢 Connected to MMN WebSocket: {self.uri}")
            self.running = True
            return True
        except Exception as e:
            print(f"🔴 Failed to connect to {self.uri}: {e}")
            return False

    async def disconnect(self):
        """Ngắt kết nối"""
        self.running = False
        if self.websocket:
            await self.websocket.close()
            print("🔴 Disconnected from MMN WebSocket")

    async def listen(self):
        """Lắng nghe events từ WebSocket"""
        if not self.websocket:
            print("❌ Not connected to WebSocket")
            return

        try:
            async for message in self.websocket:
                if not self.running:
                    break
                
                try:
                    event = json.loads(message)
                    await self.handle_event(event)
                except json.JSONDecodeError as e:
                    print(f"❌ Failed to parse message: {e}")
                except Exception as e:
                    print(f"❌ Error handling event: {e}")

        except websockets.exceptions.ConnectionClosed:
            print("🔴 Connection closed by server")
        except Exception as e:
            print(f"❌ Error in listen loop: {e}")
        finally:
            self.running = False

    async def handle_event(self, event):
        """Xử lý event nhận được"""
        event_type = event.get('type', 'unknown')
        timestamp = event.get('timestamp', 0)
        data = event.get('data', {})

        # Update stats
        if event_type in self.stats:
            self.stats[event_type] += 1
        self.stats['total_events'] += 1

        # Format timestamp
        dt = datetime.fromtimestamp(timestamp) if timestamp else datetime.now()
        time_str = dt.strftime('%H:%M:%S')

        # Handle different event types
        if event_type == 'tx_submitted':
            await self.handle_tx_submitted(time_str, data)
        elif event_type == 'tx_confirmed':
            await self.handle_tx_confirmed(time_str, data)
        elif event_type == 'tx_failed':
            await self.handle_tx_failed(time_str, data)
        elif event_type == 'block_created':
            await self.handle_block_created(time_str, data)
        elif event_type == 'mempool_stats':
            await self.handle_mempool_stats(time_str, data)
        else:
            print(f"⚠️  [{time_str}] Unknown event type: {event_type}")

    async def handle_tx_submitted(self, time_str, data):
        """Xử lý transaction submitted event"""
        tx_hash = data.get('tx_hash', 'N/A')[:16]
        tx_data = data.get('tx_data', {})
        sender = tx_data.get('sender', 'N/A')[:16] if tx_data.get('sender') else 'N/A'
        recipient = tx_data.get('recipient', 'N/A')[:16] if tx_data.get('recipient') else 'N/A'
        amount = tx_data.get('amount', 0)
        
        print(f"📝 [{time_str}] TX_SUBMITTED: {tx_hash}... | {sender}... → {recipient}... | Amount: {amount}")

    async def handle_tx_confirmed(self, time_str, data):
        """Xử lý transaction confirmed event"""
        tx_hash = data.get('tx_hash', 'N/A')[:16]
        slot = data.get('slot', 'N/A')
        tx_data = data.get('tx_data', {})
        amount = tx_data.get('amount', 0)
        
        print(f"✅ [{time_str}] TX_CONFIRMED: {tx_hash}... | Slot: {slot} | Amount: {amount}")

    async def handle_tx_failed(self, time_str, data):
        """Xử lý transaction failed event"""
        tx_hash = data.get('tx_hash', 'N/A')[:16]
        error = data.get('error', 'N/A')
        
        print(f"❌ [{time_str}] TX_FAILED: {tx_hash}... | Error: {error}")

    async def handle_block_created(self, time_str, data):
        """Xử lý block created event"""
        slot = data.get('slot', 'N/A')
        tx_count = data.get('tx_count', 0)
        transactions = data.get('transactions', [])
        
        print(f"🧱 [{time_str}] BLOCK_CREATED: Slot {slot} | TXs: {tx_count} | First TX: {transactions[0][:16] if transactions else 'None'}...")

    async def handle_mempool_stats(self, time_str, data):
        """Xử lý mempool stats event"""
        tx_count = data.get('tx_count', 0)
        max_size = data.get('max_size', 0)
        is_full = data.get('is_full', False)
        
        status = "🔴 FULL" if is_full else "🟢 OK"
        print(f"📊 [{time_str}] MEMPOOL_STATS: {tx_count}/{max_size} {status}")

    def print_stats(self):
        """In thống kê events"""
        print("\n" + "="*60)
        print("📈 EVENT STATISTICS:")
        print("="*60)
        for event_type, count in self.stats.items():
            print(f"{event_type.replace('_', ' ').title():<20}: {count:>8}")
        print("="*60)

    async def run(self, auto_reconnect=True):
        """Chạy client chính"""
        print("🚀 Starting MMN WebSocket Client...")
        print(f"🔗 Connecting to: {self.uri}")
        print("📋 Event Types: tx_submitted, tx_confirmed, tx_failed, block_created, mempool_stats")
        print("🛑 Press Ctrl+C to stop\n")

        while True:
            try:
                # Kết nối
                if not await self.connect():
                    if not auto_reconnect:
                        break
                    print("⏳ Retrying in 5 seconds...")
                    await asyncio.sleep(5)
                    continue

                # Lắng nghe events
                await self.listen()

                # Nếu không auto reconnect, thoát
                if not auto_reconnect:
                    break

                print("⏳ Reconnecting in 5 seconds...")
                await asyncio.sleep(5)

            except KeyboardInterrupt:
                print("\n🛑 Stopping client...")
                break
            except Exception as e:
                print(f"❌ Unexpected error: {e}")
                if not auto_reconnect:
                    break
                await asyncio.sleep(5)

        await self.disconnect()
        self.print_stats()
        print("👋 MMN WebSocket Client stopped.")

async def main():
    parser = argparse.ArgumentParser(description='MMN WebSocket Client')
    parser.add_argument('--uri', default='ws://localhost:8090/ws', 
                      help='WebSocket URI (default: ws://localhost:8090/ws)')
    parser.add_argument('--no-reconnect', action='store_true',
                      help='Disable auto-reconnect on connection loss')
    
    args = parser.parse_args()

    client = MMNWebSocketClient(args.uri)
    
    try:
        await client.run(auto_reconnect=not args.no_reconnect)
    except KeyboardInterrupt:
        print("\n🛑 Interrupted by user")
    except Exception as e:
        print(f"❌ Fatal error: {e}")
        sys.exit(1)

if __name__ == "__main__":
    # Cài đặt dependencies nếu cần:
    # pip install websockets
    
    try:
        import websockets
    except ImportError:
        print("❌ Missing dependency: websockets")
        print("📦 Install with: pip install websockets")
        sys.exit(1)
    
    asyncio.run(main())
