import {
  IndexerClientConfig,
  ListTransactionResponse,
  Transaction,
  TransactionDetailResponse,
  WalletDetail,
  WalletDetailResponse,
} from './types';

const API_FILTER_PARAMS = {
  ALL: 0,
  SENT: 2,
  RECEIVED: 1,
};

export class IndexerClient {
  private endpoint: string;
  private chainId: string;
  private timeout: number;
  private headers: Record<string, string>;

  constructor(config: IndexerClientConfig) {
    this.endpoint = config.endpoint;
    this.chainId = config.chainId;
    this.timeout = config.timeout || 30000;

    // Minimal headers to avoid CORS preflight issues
    this.headers = {
      Accept: 'application/json',
      ...(config.headers || {}),
    };
  }

  /**
   * Make HTTP request with automatic CORS handling
   * Works out-of-the-box without CORS configuration
   * @param method - HTTP method (GET or POST)
   * @param path - API endpoint path
   * @param params - URL query parameters
   * @param body - Request body for POST requests
   * @returns Promise resolving to response data
   */
  private async makeRequest<T>(
    method: 'GET' | 'POST',
    path: string,
    params?: Record<string, string | number>,
    body?: any
  ): Promise<T> {
    // Build full URL
    let url = `${this.endpoint}/${path}`;

    // Add query parameters
    if (params && Object.keys(params).length > 0) {
      const searchParams = new URLSearchParams();
      Object.entries(params).forEach(([key, value]) => {
        searchParams.append(key, String(value));
      });
      url += `?${searchParams.toString()}`;
    }

    // Create AbortController for timeout
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeout);

    try {
      // Simple fetch with automatic CORS handling
      const requestOptions: RequestInit = {
        method,
        mode: 'cors',
        credentials: 'omit',
        signal: controller.signal,
        headers: {
          Accept: 'application/json',
          ...this.headers,
        },
      };

      // Add body and Content-Type for POST requests
      if (method === 'POST' && body) {
        requestOptions.body = JSON.stringify(body);
        (requestOptions.headers as Record<string, string>)['Content-Type'] =
          'application/json';
      }

      const response = await fetch(url, requestOptions);
      clearTimeout(timeoutId);

      // Handle response errors
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      // Parse JSON response
      const data = await response.json();
      return data as T;
    } catch (error) {
      clearTimeout(timeoutId);
      if (error instanceof Error) {
        if (error.name === 'AbortError') {
          throw new Error(`Request timeout after ${this.timeout}ms`);
        }
        throw error;
      }

      throw new Error('Request failed');
    }
  }

  async getTransactionByHash(hash: string): Promise<Transaction> {
    const path = `${this.chainId}/tx/${hash}/detail`;
    const res = await this.makeRequest<TransactionDetailResponse>('GET', path);
    return res.data.transaction;
  }

  async getTransactionByWallet(
    wallet: string,
    page = 1,
    limit = 50,
    filter: number,
    sortBy = 'transaction_timestamp',
    sortOrder: 'asc' | 'desc' = 'desc'
  ): Promise<ListTransactionResponse> {
    if (!wallet) {
      throw new Error('wallet address cannot be empty');
    }

    if (page < 1) page = 1;
    if (limit <= 0) limit = 50;
    if (limit > 1000) limit = 1000;

    const params: Record<string, string | number> = {
      page: page - 1,
      limit,
      sort_by: sortBy,
      sort_order: sortOrder,
    };

    switch (filter) {
      case API_FILTER_PARAMS.ALL:
        params['wallet_address'] = wallet;
        break;
      case API_FILTER_PARAMS.SENT:
        params['filter_from_address'] = wallet;
        break;
      case API_FILTER_PARAMS.RECEIVED:
        params['filter_to_address'] = wallet;
        break;
      default:
        break;
    }

    const path = `${this.chainId}/transactions`;
    return this.makeRequest<ListTransactionResponse>('GET', path, params);
  }

  async getWalletDetail(wallet: string): Promise<WalletDetail> {
    if (!wallet) {
      throw new Error('wallet address cannot be empty');
    }

    const path = `${this.chainId}/wallets/${wallet}/detail`;
    const res = await this.makeRequest<WalletDetailResponse>('GET', path);
    return res.data;
  }
}
