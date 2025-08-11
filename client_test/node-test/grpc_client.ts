import { GrpcTransport } from '@protobuf-ts/grpc-transport';
import { ChannelCredentials } from '@grpc/grpc-js';
import {
  TxServiceClient,
  ITxServiceClient,
} from './generated/tx.client';
import {
  AccountServiceClient,
  IAccountServiceClient,
} from './generated/account.client';

import type {
  TxMsg as GenTxMsg,
  SignedTxMsg as GenSignedTxMsg,
  TxResponse as GenTxResponse,
  AddTxResponse as GenAddTxResponse,
  GetTransactionStatusRequest as GenGetTxStatusRequest,
  GetTransactionStatusResponse as GenGetTxStatusResponse,
  SubscribeTransactionStatusRequest as GenSubscribeTxStatusRequest,
  TransactionStatusUpdate as GenTxStatusUpdate,
} from './generated/tx';
import { TransactionStatus as GenTxStatusEnum } from './generated/tx';
import type {
  GetAccountRequest as GenGetAccountRequest,
  GetAccountResponse as GenGetAccountResponse,
  GetTxHistoryRequest as GenGetTxHistoryRequest,
  GetTxHistoryResponse as GenGetTxHistoryResponse,
} from './generated/account';

export class GrpcClient {
  private transport: GrpcTransport;
  private txClient: ITxServiceClient;
  private accountClient: IAccountServiceClient;

  constructor(serverAddress: string) {
    this.transport = new GrpcTransport({
      host: serverAddress,
      channelCredentials: ChannelCredentials.createInsecure(),
    });
    this.txClient = new TxServiceClient(this.transport);
    this.accountClient = new AccountServiceClient(this.transport);
  }

  async broadcastTransaction(
    txMsg: {
      type: number; sender: string; recipient: string; amount: number; timestamp: number; text_data: string; nonce: number;
    },
    signature: string
  ): Promise<{ ok: boolean; error?: string }> {
    const genTx: GenTxMsg = {
      type: txMsg.type,
      sender: txMsg.sender,
      recipient: txMsg.recipient,
      amount: BigInt(txMsg.amount),
      timestamp: BigInt(txMsg.timestamp),
      textData: txMsg.text_data,
      nonce: BigInt(txMsg.nonce),
    };
    const req: GenSignedTxMsg = { txMsg: genTx, signature };

    const call = this.txClient.txBroadcast(req as any as GenSignedTxMsg);
    const res: GenTxResponse = await call.response;
    return { ok: res.ok, error: res.error };
  }

  async addTransaction(
    txMsg: {
      type: number; sender: string; recipient: string; amount: number; timestamp: number; text_data: string; nonce: number;
    },
    signature: string
  ): Promise<{ ok: boolean; tx_hash?: string; error?: string }> {
    const genTx: GenTxMsg = {
      type: txMsg.type,
      sender: txMsg.sender,
      recipient: txMsg.recipient,
      amount: BigInt(txMsg.amount),
      timestamp: BigInt(txMsg.timestamp),
      textData: txMsg.text_data,
      nonce: BigInt(txMsg.nonce),
    };
    const req: GenSignedTxMsg = { txMsg: genTx, signature };

    const call = this.txClient.addTx(req as any as GenSignedTxMsg);
    const res: GenAddTxResponse = await call.response;
    return { ok: res.ok, tx_hash: res.txHash, error: res.error };
  }

  async getAccount(address: string): Promise<{ address: string; balance: string; nonce: string }> {
    const req: GenGetAccountRequest = { address };
    const call = this.accountClient.getAccount(req);
    const res: GenGetAccountResponse = await call.response;
    return {
      address: res.address,
      balance: res.balance.toString(),
      nonce: res.nonce.toString(),
    };
  }

  async getTxHistory(
    address: string,
    limit: number,
    offset: number,
    filter: number
  ): Promise<{ total: number; txs: { sender: string; recipient: string; amount: string; nonce: string; timestamp: string; status: string }[] }> {
    const req: GenGetTxHistoryRequest = { address, limit, offset, filter };
    const call = this.accountClient.getTxHistory(req);
    const res: GenGetTxHistoryResponse = await call.response;
    return {
      total: res.total,
      txs: res.txs.map(tx => ({
        sender: tx.sender,
        recipient: tx.recipient,
        amount: tx.amount.toString(),
        nonce: tx.nonce.toString(),
        timestamp: tx.timestamp.toString(),
        status: ['PENDING','CONFIRMED','FINALIZED','FAILED'][tx.status] || 'PENDING',
      })),
    };
  }

  // Transaction status methods (real implementations)
  async getTransactionStatus(txHash: string): Promise<{
    tx_hash: string;
    status: string;
    block_slot?: string;
    block_hash?: string;
    confirmations?: string;
    error_message?: string;
    timestamp?: string;
  }> {
    const req: GenGetTxStatusRequest = { txHash };
    const call = this.txClient.getTransactionStatus(req);
    const res: GenGetTxStatusResponse = await call.response;

    const statusStr = GenTxStatusEnum[res.status] as unknown as string;

    return {
      tx_hash: res.txHash,
      status: statusStr || 'UNKNOWN',
      block_slot: res.blockSlot ? res.blockSlot.toString() : undefined,
      block_hash: res.blockHash || undefined,
      confirmations: res.confirmations ? res.confirmations.toString() : undefined,
      error_message: res.errorMessage || undefined,
      timestamp: res.timestamp ? res.timestamp.toString() : undefined,
    };
  }

  subscribeTransactionStatus(
    txHash: string,
    timeoutSeconds: number,
    onUpdate: (update: {
      tx_hash: string;
      status: string;
      block_slot?: string;
      block_hash?: string;
      confirmations?: string;
      error_message?: string;
      timestamp?: string;
    }) => void,
    onError: (error: any) => void,
    onComplete: () => void
  ): () => void {
    const req: GenSubscribeTxStatusRequest = { txHash, timeoutSeconds };
    const abortController = new AbortController();
    const call = this.txClient.subscribeTransactionStatus(req, { abort: abortController.signal });

    (async () => {
      try {
        console.log("call.responses", call.responses)
        for await (const update of call.responses as AsyncIterable<GenTxStatusUpdate>) {
          const statusStr = GenTxStatusEnum[update.status] as unknown as string;
          onUpdate({
            tx_hash: update.txHash,
            status: statusStr || 'UNKNOWN',
            block_slot: update.blockSlot ? update.blockSlot.toString() : undefined,
            block_hash: update.blockHash || undefined,
            confirmations: update.confirmations ? update.confirmations.toString() : undefined,
            error_message: update.errorMessage || undefined,
            timestamp: update.timestamp ? update.timestamp.toString() : undefined,
          });
        }
        onComplete();
      } catch (err: any) {
        if (abortController.signal.aborted) {
          // treat as completed due to unsubscribe
          onComplete();
          return;
        }
        onError(err);
      }
    })();

    // Return unsubscribe that cancels the streaming call
    return () => {
      try {
        abortController.abort();
      } catch (_e) {
        // ignore
      }
    };
  }

  close(): void {
    this.transport.close();
  }
}
