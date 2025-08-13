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
  AddTxResponse as GenAddTxResponse,
  TransactionStatusInfo as GenTxStatusInfo,
  SubscribeTransactionStatusRequest as GenSubscribeTxStatusRequest,
} from './generated/tx';
import { TransactionStatus as GenTxStatusEnum } from './generated/tx';
import type {
  GetAccountRequest as GenGetAccountRequest,
  GetAccountResponse as GenGetAccountResponse,
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





  subscribeTransactionStatus(
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
    const req: GenSubscribeTxStatusRequest = {}; // Empty request for all transactions
    const abortController = new AbortController();
    const call = this.txClient.subscribeTransactionStatus(req, { abort: abortController.signal });

    (async () => {
      try {
        for await (const update of call.responses as AsyncIterable<GenTxStatusInfo>) {
          // Log the raw update from the server (with BigInt handling)
          const serializableUpdate = {
            txHash: update.txHash,
            status: update.status,
            blockSlot: update.blockSlot?.toString(),
            blockHash: update.blockHash,
            confirmations: update.confirmations?.toString(),
            errorMessage: update.errorMessage,
            timestamp: update.timestamp?.toString(),
          };
          console.log(`🔄 Raw Update from Server:`, JSON.stringify(serializableUpdate, null, 2));
          
          const statusStr = GenTxStatusEnum[update.status] as unknown as string;
          const processedUpdate = {
            tx_hash: update.txHash,
            status: statusStr || 'UNKNOWN',
            block_slot: update.blockSlot ? update.blockSlot.toString() : undefined,
            block_hash: update.blockHash || undefined,
            confirmations: update.confirmations ? update.confirmations.toString() : undefined,
            error_message: update.errorMessage || undefined,
            timestamp: update.timestamp ? update.timestamp.toString() : undefined,
          };
          
          // Log the processed update
          console.log(`📤 Processing Update:`, JSON.stringify(processedUpdate, null, 2));
          
          onUpdate(processedUpdate);
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
