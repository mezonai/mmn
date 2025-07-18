import axios from "axios";
import crypto from "crypto";
import nacl from "tweetnacl";

// Fixed Ed25519 keypair for faucet (hardcoded for genesis config)
const faucetPrivateKeyHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee";
const faucetPrivateKeyDer = Buffer.from(faucetPrivateKeyHex, "hex");
// Extract the last 32 bytes as the Ed25519 seed
const faucetSeed = faucetPrivateKeyDer.slice(-32);
const faucetKeyPair = nacl.sign.keyPair.fromSeed(faucetSeed);
const faucetPublicKeyHex = Buffer.from(faucetKeyPair.publicKey).toString("hex");
const faucetPrivateKey = crypto.createPrivateKey({
  key: faucetPrivateKeyDer,
  format: "der",
  type: "pkcs8"
});

// Generate Ed25519 keypair for recipient1
const recipientSeed1 = crypto.randomBytes(32);
const recipientKeyPair1 = nacl.sign.keyPair.fromSeed(recipientSeed1);
const recipientPublicKeyHex1 = Buffer.from(recipientKeyPair1.publicKey).toString("hex");
const recipientPrivateKey1 = crypto.createPrivateKey({
  key: Buffer.concat([
    Buffer.from("302e020100300506032b657004220420", "hex"),
    recipientSeed1
  ]),
  format: "der",
  type: "pkcs8"
});

// Generate Ed25519 keypair for recipient2
const recipientSeed2 = crypto.randomBytes(32);
const recipientKeyPair2 = nacl.sign.keyPair.fromSeed(recipientSeed2);
const recipientPublicKeyHex2 = Buffer.from(recipientKeyPair2.publicKey).toString("hex");
const recipientPrivateKey2 = crypto.createPrivateKey({
  key: Buffer.concat([
    Buffer.from("302e020100300506032b657004220420", "hex"),
    recipientSeed2
  ]),
  format: "der",
  type: "pkcs8"
});

const API_URL = "http://localhost:8001";

interface Tx {
  type: number;
  sender: string;
  recipient: string;
  amount: number;
  timestamp: number;
  textData: string;
  nonce: number;
  signature: string;
}

function buildTx(sender: string, recipient: string, amount: number, textData: string, nonce: number, type: number): Tx {
  return {
    type:      type,
    sender:    sender,
    recipient: recipient,
    amount:    amount,
    timestamp: Math.floor(Date.now() / 1000),
    textData:  textData,
    nonce:     nonce,
    signature: "", // to be filled
  };
}

function serializeTx(tx: Tx): Buffer {
  // Must match Go's Transaction.Serialize() method
  const serializedData = {
    type:      tx.type,
    sender:    tx.sender,
    recipient: tx.recipient,
    amount:    tx.amount,
    timestamp: tx.timestamp,
    textData:  tx.textData,
    nonce:     tx.nonce,
  };
  return Buffer.from("1234567890");
}

function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  const serializedData = serializeTx(tx);
  const signature = crypto.sign(null, serializedData, privateKey);
  return signature.toString("hex");
}

function verifyTx(tx: Tx, publicKeyHex: string): boolean {
  // Reconstruct the DER-encoded SPKI public key from the raw 32-byte hex key.
  const spkiPrefix = Buffer.from("302a300506032b6570032100", "hex");
  const publicKeyDer = Buffer.concat([spkiPrefix, Buffer.from(publicKeyHex, "hex")]);
  const publicKey = crypto.createPublicKey({
    key: publicKeyDer,
    format: "der",
    type: "spki"
  });

  const signature = Buffer.from(tx.signature, "hex");
  const serializedData = serializeTx(tx);

  console.log("serializedData:", serializedData);
  console.log("signature:", signature);
  return crypto.verify(null, serializedData, publicKey, signature);
}

async function sendTx(tx: Tx) {
  const addResp = await axios.post(`${API_URL}/txs`, tx);
  console.log("Add tx response:", addResp.status, addResp.data);
  return addResp.data.id;
}

const FaucetTxType = 1;
const TransferTxType = 0;

async function main() {
  // 1. Seed recipient1
  const amount = 100;
  const nonce = 0;
  let tx = buildTx(faucetPublicKeyHex, recipientPublicKeyHex1, amount, "Seed 100 amount", nonce, FaucetTxType);
  tx.signature = signTx(tx, faucetPrivateKey);
  console.log("Verifying tx 1 locally:", verifyTx(tx, faucetPublicKeyHex));
  const txId = await sendTx(tx);
  console.log("Tx sent:", txId);

  // 2. Seed recipient2
  const amount2 = 200;
  const tx2 = buildTx(faucetPublicKeyHex, recipientPublicKeyHex2, amount2, "Seed 200 amount", nonce, FaucetTxType);
  tx2.signature = signTx(tx2, faucetPrivateKey);
  console.log("Verifying tx 2 locally:", verifyTx(tx2, faucetPublicKeyHex));
  const txId2 = await sendTx(tx2);
  console.log("Tx sent:", txId2);
  await new Promise((resolve) => setTimeout(resolve, 200));

  // 3. Send tx from recipient1 to recipient2
  const amount3 = 10;
  const tx3 = buildTx(recipientPublicKeyHex1, recipientPublicKeyHex2, amount3, "Send 10 amount", nonce+1, TransferTxType);
  tx3.signature = signTx(tx3, recipientPrivateKey1);
  console.log("Verifying tx 3 locally:", verifyTx(tx3, recipientPublicKeyHex1));
  const txId3 = await sendTx(tx3);
  console.log("Tx sent:", txId3);

  // Sleep 5 seconds and count down
  for (let i = 5; i > 0; i--) {
    console.log(`Sleeping ${i} seconds...`);
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }

  // Get account for recipient1
  const account = await axios.get(`${API_URL}/account?addr=${recipientPublicKeyHex1}`);
  console.log("Account:", account.data);

  // Get account for recipient2
  const account2 = await axios.get(`${API_URL}/account?addr=${recipientPublicKeyHex2}`);
  console.log("Account:", account2.data);

  // Get account for faucet
  const faucetAccount = await axios.get(`${API_URL}/account?addr=${faucetPublicKeyHex}`);
  console.log("Faucet account:", faucetAccount.data);
}

main().catch(console.error);
