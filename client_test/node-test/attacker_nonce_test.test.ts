import { TransactionStatus } from './generated/tx';
import { GrpcClient } from './grpc_client';
import { TransactionTracker } from './transaction_tracker';
import {
  TxTypeTransfer,
  generateTestAccount,
  getAccountBalance,
  getCurrentNonce,
  buildTx,
  signTx,
  sendTxViaGrpc,
  fundAccount,
  waitForTransaction,
} from './utils';

const GRPC_SERVER_ADDRESS = '127.0.0.1:9001';

describe('Attacker Nonce Simulation Tests', () => {
  let grpcClient: GrpcClient;
  let tracker: TransactionTracker;

  beforeAll(() => {
    grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
    tracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS, debug: true });
    tracker.trackTransactions();
  });

  afterAll(() => {
    tracker.close();
    grpcClient.close();
  });

  test('Nonce replacement/supersede is rejected (same nonce, different payload)', async () => {
    const sender = generateTestAccount();
    const rcv1 = generateTestAccount();
    const rcv2 = generateTestAccount();

    expect((await fundAccount(grpcClient, sender.publicKeyHex, 1000, 'nonce-replace')).ok).toBe(true);

    const cur = await getCurrentNonce(grpcClient, sender.publicKeyHex, 'pending');
    const n = cur + 1;

    // First tx
    const tx1 = buildTx(sender.publicKeyHex, rcv1.publicKeyHex, 100, 'tx1', n, TxTypeTransfer);
    tx1.signature = signTx(tx1, sender.privateKey);
    const res1 = await sendTxViaGrpc(grpcClient, tx1);
    expect(res1.ok).toBe(true);
    if (res1.tx_hash) await tracker.waitForTerminalStatus(res1.tx_hash);

    // Attempt replacement with different payload but same nonce
    const tx2 = buildTx(sender.publicKeyHex, rcv2.publicKeyHex, 200, 'tx2-replace', n, TxTypeTransfer);
    tx2.signature = signTx(tx2, sender.privateKey);
    const res2 = await sendTxViaGrpc(grpcClient, tx2);
    expect(res2.ok).toBe(false);
  }, 60000);

  test('Same-nonce duplicate submitted via multiple clients: only one accepted', async () => {
    const clientA = new GrpcClient(GRPC_SERVER_ADDRESS);
    const clientB = new GrpcClient(GRPC_SERVER_ADDRESS);
    const sender = generateTestAccount();
    const rcv = generateTestAccount();

    expect((await fundAccount(clientA, sender.publicKeyHex, 1000, 'multi-client-dup')).ok).toBe(true);

    const cur = await getCurrentNonce(clientA, sender.publicKeyHex, 'pending');
    const n = cur + 1;

    const tx = buildTx(sender.publicKeyHex, rcv.publicKeyHex, 123, 'multi-client', n, TxTypeTransfer);
    tx.signature = signTx(tx, sender.privateKey);

    // Submit to two clients nearly simultaneously
    const [r1, r2] = await Promise.all([sendTxViaGrpc(clientA, tx), sendTxViaGrpc(clientB, tx)]);
    const successCount = (r1.ok ? 1 : 0) + (r2.ok ? 1 : 0);
    expect(successCount).toBe(1);

    clientA.close();
    clientB.close();
  }, 60000);

  test('Pending nonce poisoning resilience keeps next valid nonce working', async () => {
    const sender = generateTestAccount();
    const rcv = generateTestAccount();

    expect((await fundAccount(grpcClient, sender.publicKeyHex, 1000, 'pending-poison')).ok).toBe(true);

    const cur = await getCurrentNonce(grpcClient, sender.publicKeyHex, 'pending');
    const base = cur + 1;

    // Poison with future nonces within reasonable limit
    const futNonces = [base + 5, base + 6, base + 7, base + 8];
    for (const fn of futNonces) {
      const ft = buildTx(sender.publicKeyHex, rcv.publicKeyHex, 1, `poison-${fn}`, fn, TxTypeTransfer);
      ft.signature = signTx(ft, sender.privateKey);
      await sendTxViaGrpc(grpcClient, ft);
    }

    // Now send the correct next nonce, it must still be accepted
    const valid = buildTx(sender.publicKeyHex, rcv.publicKeyHex, 10, 'valid-next', base, TxTypeTransfer);
    valid.signature = signTx(valid, sender.privateKey);
    const vr = await sendTxViaGrpc(grpcClient, valid);
    expect(vr.ok).toBe(true);
    if (vr.tx_hash) {
      const st = await tracker.waitForTerminalStatus(vr.tx_hash);
      expect([TransactionStatus.FINALIZED, TransactionStatus.FAILED]).toContain(st.status);
    }
  }, 60000);

  test('Invalid-signature flood on same nonce does not block valid tx', async () => {
    const sender = generateTestAccount();
    const rcv = generateTestAccount();
    const wrong = generateTestAccount();

    expect((await fundAccount(grpcClient, sender.publicKeyHex, 1000, 'invalid-sig-flood')).ok).toBe(true);

    const cur = await getCurrentNonce(grpcClient, sender.publicKeyHex, 'pending');
    const n = cur + 1;

    // Flood invalid signatures on same nonce
    let rejects = 0;
    for (let i = 0; i < 20; i++) {
      const bad = buildTx(sender.publicKeyHex, rcv.publicKeyHex, 1, `bad-${i}`, n, TxTypeTransfer);
      // sign with wrong key
      bad.signature = signTx(bad, wrong.privateKey);
      const r = await sendTxViaGrpc(grpcClient, bad);
      if (!r.ok) rejects++;
    }
    expect(rejects).toBeGreaterThan(0);

    // Now send the valid tx with same nonce
    const good = buildTx(sender.publicKeyHex, rcv.publicKeyHex, 5, 'good', n, TxTypeTransfer);
    good.signature = signTx(good, sender.privateKey);
    const gr = await sendTxViaGrpc(grpcClient, good);
    expect(gr.ok).toBe(true);
    if (gr.tx_hash) await tracker.waitForTerminalStatus(gr.tx_hash);
  }, 60000);
  test('Duplicate nonce flood from one attacker address is rejected', async () => {
    const attacker = generateTestAccount();
    const victim = generateTestAccount();

    const fundResponse = await fundAccount(grpcClient, attacker.publicKeyHex, 1000, 'dup-nonce-flood');
    expect(fundResponse.ok).toBe(true);

    const current = await getCurrentNonce(grpcClient, attacker.publicKeyHex, 'pending');
    const nextNonce = current + 1;

    // First tx with nonce nextNonce should succeed
    const baseTx = buildTx(attacker.publicKeyHex, victim.publicKeyHex, 10, 'base', nextNonce, TxTypeTransfer);
    baseTx.signature = signTx(baseTx, attacker.privateKey);
    const baseRes = await sendTxViaGrpc(grpcClient, baseTx);
    expect(baseRes.ok).toBe(true);
    if (baseRes.tx_hash) {
      const st = await tracker.waitForTerminalStatus(baseRes.tx_hash);
      expect([TransactionStatus.FINALIZED, TransactionStatus.FAILED]).toContain(st.status);
    }

    // Flood 30 duplicates with the SAME nonce
    const attempts = 30;
    let rejected = 0;
    for (let i = 0; i < attempts; i++) {
      const dupTx = buildTx(attacker.publicKeyHex, victim.publicKeyHex, 10, `dup-${i}`, nextNonce, TxTypeTransfer);
      dupTx.signature = signTx(dupTx, attacker.privateKey);
      const res = await sendTxViaGrpc(grpcClient, dupTx);
      if (!res.ok) rejected++;
    }

    // Expect all duplicates to be rejected by mempool or ledger
    expect(rejected).toBe(attempts);
  }, 60000);

  test('Future nonce gap flood is limited and does not break ordering', async () => {
    const attacker = generateTestAccount();
    const target = generateTestAccount();

    const fundResponse = await fundAccount(grpcClient, attacker.publicKeyHex, 1000, 'future-gap-flood');
    expect(fundResponse.ok).toBe(true);

    const current = await getCurrentNonce(grpcClient, attacker.publicKeyHex, 'pending');
    const nextNonce = current + 1;

    // Send a reasonable ready tx first (nonce = next)
    const readyTx = buildTx(attacker.publicKeyHex, target.publicKeyHex, 10, 'ready', nextNonce, TxTypeTransfer);
    readyTx.signature = signTx(readyTx, attacker.privateKey);
    const readyRes = await sendTxViaGrpc(grpcClient, readyTx);
    expect(readyRes.ok).toBe(true);

    // Flood many future-nonce txs to create gaps (e.g., next+5 .. next+70)
    const start = nextNonce + 5;
    const end = nextNonce + 70; // beyond MaxFutureNonce=64 should be rejected by server
    let accepted = 0;
    let rejected = 0;
    for (let n = start; n <= end; n++) {
      const futTx = buildTx(attacker.publicKeyHex, target.publicKeyHex, 1, `future-${n}`, n, TxTypeTransfer);
      futTx.signature = signTx(futTx, attacker.privateKey);
      const res = await sendTxViaGrpc(grpcClient, futTx);
      if (res.ok) accepted++; else rejected++;
    }

    // Expect some accepted within limit and rejections beyond limit
    expect(accepted).toBeGreaterThan(0);
    expect(rejected).toBeGreaterThan(0);

    // Process the ready tx and ensure balances remain consistent
    if (readyRes.tx_hash) {
      await tracker.waitForTerminalStatus(readyRes.tx_hash);
    }

    await waitForTransaction(2000);

    const attackerBalance = await getAccountBalance(grpcClient, attacker.publicKeyHex);
    const targetBalance = await getAccountBalance(grpcClient, target.publicKeyHex);
    expect(attackerBalance + targetBalance).toBe(1000);
  }, 90000);

  test('Same nonce across different senders is allowed', async () => {
    const sender1 = generateTestAccount();
    const sender2 = generateTestAccount();
    const recipient = generateTestAccount();

    expect((await fundAccount(grpcClient, sender1.publicKeyHex, 1000, 'cross-sender-dup-1')).ok).toBe(true);
    expect((await fundAccount(grpcClient, sender2.publicKeyHex, 1000, 'cross-sender-dup-2')).ok).toBe(true);

    const nonce1 = (await getCurrentNonce(grpcClient, sender1.publicKeyHex, 'pending')) + 1;
    const nonce2 = (await getCurrentNonce(grpcClient, sender2.publicKeyHex, 'pending')) + 1;

    // Force both to use nonce=1 relative to their account state
    const tx1 = buildTx(sender1.publicKeyHex, recipient.publicKeyHex, 100, 's1', nonce1, TxTypeTransfer);
    tx1.signature = signTx(tx1, sender1.privateKey);

    const tx2 = buildTx(sender2.publicKeyHex, recipient.publicKeyHex, 100, 's2', nonce2, TxTypeTransfer);
    tx2.signature = signTx(tx2, sender2.privateKey);

    const [r1, r2] = await Promise.all([sendTxViaGrpc(grpcClient, tx1), sendTxViaGrpc(grpcClient, tx2)]);
    expect(r1.ok).toBe(true);
    expect(r2.ok).toBe(true);
  }, 60000);
});


