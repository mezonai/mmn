# Flow Xác Thực Node (Authentication Flow)

## Sơ Đồ Tổng Quan

```mermaid
sequenceDiagram
    participant NodeA as Node A (Initiator)
    participant NodeB as Node B (Responder)
    participant PSM as Peer Scoring Manager
    participant AC as Access Control

    Note over NodeA,AC: Bước 1: Khởi tạo xác thực
    NodeA->>NodeA: Tạo random challenge (32 bytes)
    NodeA->>NodeA: Tạo random nonce (8 bytes)
    NodeA->>NodeA: Lưu challenge vào pendingChallenges
    NodeA->>NodeA: Tạo AuthChallenge object

    Note over NodeA,AC: Bước 2: Gửi challenge
    NodeA->>NodeB: Gửi challenge qua stream
    Note right of NodeA: Protocol: /auth/handshake/1.0.0

    Note over NodeA,AC: Bước 3: Xử lý challenge
    NodeB->>NodeB: Validate challenge
    Note right of NodeB: - Kiểm tra version<br/>- Kiểm tra chain ID<br/>- Kiểm tra timestamp<br/>- Kiểm tra format
    NodeB->>NodeB: Tạo data để ký: challenge + nonce + chainID
    NodeB->>NodeB: Ký bằng Ed25519 private key
    NodeB->>NodeB: Tạo AuthResponse với signature

    Note over NodeA,AC: Bước 4: Gửi response
    NodeB->>NodeA: Gửi response về

    Note over NodeA,AC: Bước 5: Xác thực response
    NodeA->>NodeA: Validate response
    Note right of NodeA: - Kiểm tra timestamp<br/>- Kiểm tra challenge match<br/>- Kiểm tra version/chain ID
    NodeA->>NodeA: Verify Ed25519 signature

    alt Xác thực thành công
        NodeA->>NodeA: Thêm vào authenticatedPeers
        NodeA->>NodeA: Xóa khỏi pendingChallenges
        NodeA->>NodeA: Cập nhật PeerInfo.IsAuthenticated = true
        NodeA->>PSM: UpdatePeerScore(peerID, "auth_success")
        Note right of PSM: +5.0 điểm
        NodeA->>AC: AutoAddToAllowlistIfBootstrap
        Note right of AC: Nếu là bootstrap peer
    else Xác thực thất bại
        NodeA->>PSM: UpdatePeerScore(peerID, "auth_failure")
        Note right of PSM: -20.0 điểm
    end
```

## Sơ Đồ Chi Tiết Cấu Trúc Dữ Liệu

```mermaid
classDiagram
    class AuthChallenge {
        +string Version
        +[]byte Challenge
        +string PeerID
        +string PublicKey
        +int64 Timestamp
        +uint64 Nonce
        +string ChainID
    }

    class AuthResponse {
        +string Version
        +[]byte Challenge
        +[]byte Signature
        +string PeerID
        +string PublicKey
        +int64 Timestamp
        +uint64 Nonce
        +string ChainID
    }

    class AuthenticatedPeer {
        +peer.ID PeerID
        +ed25519.PublicKey PublicKey
        +time.Time AuthTimestamp
        +bool IsValid
    }

    class Libp2pNetwork {
        +map[peer.ID]*AuthenticatedPeer authenticatedPeers
        +map[peer.ID][]byte pendingChallenges
        +sync.RWMutex authMu
        +sync.RWMutex challengeMu
        +InitiateAuthentication()
        +handleAuthStream()
        +handleAuthChallenge()
        +handleAuthResponse()
    }

    Libp2pNetwork --> AuthChallenge
    Libp2pNetwork --> AuthResponse
    Libp2pNetwork --> AuthenticatedPeer
```

## Sơ Đồ Luồng Xử Lý Challenge

```mermaid
flowchart TD
    A[Node A: InitiateAuthentication] --> B[Tạo random challenge 32 bytes]
    B --> C[Tạo random nonce 8 bytes]
    C --> D[Lưu vào pendingChallenges]
    D --> E[Tạo AuthChallenge object]
    E --> F[Gửi challenge qua stream]
    
    F --> G[Node B: handleAuthStream]
    G --> H[Parse challenge message]
    H --> I{Validate challenge?}
    
    I -->|No| J[Log error & return]
    I -->|Yes| K[Tạo data để ký: challenge + nonce + chainID]
    K --> L[Ký bằng Ed25519 private key]
    L --> M[Tạo AuthResponse với signature]
    M --> N[Gửi response về Node A]
    
    N --> O[Node A: handleAuthResponse]
    O --> P[Parse response message]
    P --> Q{Validate response?}
    
    Q -->|No| R[Log error & return]
    Q -->|Yes| S[Verify Ed25519 signature]
    
    S --> T{Signature valid?}
    T -->|No| U[UpdatePeerScore auth_failure]
    T -->|Yes| V[Thêm vào authenticatedPeers]
    V --> W[Xóa khỏi pendingChallenges]
    W --> X[Cập nhật PeerInfo.IsAuthenticated]
    X --> Y[UpdatePeerScore auth_success]
    Y --> Z[AutoAddToAllowlistIfBootstrap]
```

## Sơ Đồ Tích Hợp Với Peer Scoring

```mermaid
graph LR
    subgraph "Authentication Events"
        A1[auth_success]
        A2[auth_failure]
    end
    
    subgraph "Peer Scoring Manager"
        PSM[PeerScoringManager]
        SCORE[Score Calculation]
        DECAY[Score Decay]
        AUTO[Auto Access Control]
    end
    
    subgraph "Access Control"
        AL[Allowlist]
        BL[Blacklist]
        THRESHOLD[Threshold Check]
    end
    
    A1 --> PSM
    A2 --> PSM
    PSM --> SCORE
    SCORE --> DECAY
    DECAY --> AUTO
    AUTO --> THRESHOLD
    THRESHOLD --> AL
    THRESHOLD --> BL
```

## Sơ Đồ Bảo Mật & Validation

```mermaid
flowchart TD
    subgraph "Challenge Validation"
        CV1[Version Check]
        CV2[Chain ID Check]
        CV3[Timestamp Check]
        CV4[Format Check]
    end
    
    subgraph "Response Validation"
        RV1[Timestamp Check]
        RV2[Challenge Match]
        RV3[Version Check]
        RV4[Signature Verify]
    end
    
    subgraph "Security Measures"
        SM1[Ed25519 Cryptography]
        SM2[Random Challenge]
        SM3[Random Nonce]
        SM4[Timeout Protection]
        SM5[Replay Attack Prevention]
    end
    
    CV1 --> CV2 --> CV3 --> CV4
    RV1 --> RV2 --> RV3 --> RV4
    SM1 --> SM2 --> SM3 --> SM4 --> SM5
```

## Constants & Configuration

```mermaid
graph TD
    subgraph "Protocol Constants"
        PC1[AuthProtocol: /auth/handshake/1.0.0]
        PC2[AuthVersion: 1.0.0]
        PC3[DefaultChainID: mmn]
    end
    
    subgraph "Security Constants"
        SC1[AuthChallengeSize: 32 bytes]
        SC2[AuthTimeout: 600 seconds]
        SC3[Ed25519 Key Size: 32 bytes]
    end
    
    subgraph "Scoring Constants"
        SPC1[auth_success: +5.0]
        SPC2[auth_failure: -20.0]
        SPC3[AutoAllowlistThreshold: 50.0]
        SPC4[AutoBlacklistThreshold: -20.0]
    end
```

## Error Handling Flow

```mermaid
flowchart TD
    A[Authentication Attempt] --> B{Connection Success?}
    B -->|No| C[Connection Error]
    B -->|Yes| D{Challenge Valid?}
    
    D -->|No| E[Challenge Validation Error]
    D -->|Yes| F{Response Valid?}
    
    F -->|No| G[Response Validation Error]
    F -->|Yes| H{Signature Valid?}
    
    H -->|No| I[Signature Verification Failed]
    H -->|Yes| J[Authentication Success]
    
    C --> K[Log Error & Retry]
    E --> L[Log Error & Penalize]
    G --> M[Log Error & Penalize]
    I --> N[UpdatePeerScore auth_failure]
    J --> O[UpdatePeerScore auth_success]
``` 