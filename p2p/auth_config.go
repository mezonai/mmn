package p2p

// AuthConfig holds authentication configuration
type AuthConfig struct {
	ChainID           string
	AuthTimeout       int
	AuthChallengeSize int
	StrictMode        bool
}

// DefaultAuthConfig returns the default authentication configuration
func DefaultAuthConfig() *AuthConfig {
	return &AuthConfig{
		ChainID:           DefaultChainID,
		AuthTimeout:       AuthTimeout,
		AuthChallengeSize: AuthChallengeSize,
		StrictMode:        false,
	}
}

// GetChainID returns the configured chain ID
func (ac *AuthConfig) GetChainID() string {
	return ac.ChainID
}

// GetAuthTimeout returns the configured authentication timeout
func (ac *AuthConfig) GetAuthTimeout() int {
	return ac.AuthTimeout
}

// GetAuthChallengeSize returns the configured challenge size
func (ac *AuthConfig) GetAuthChallengeSize() int {
	return ac.AuthChallengeSize
}

// IsStrictMode returns whether strict mode is enabled
func (ac *AuthConfig) IsStrictMode() bool {
	return ac.StrictMode
}
