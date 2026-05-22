package service

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/model"
)

const (
	AlipayQRLoginPending    = "pending"
	AlipayQRLoginAuthorized = "authorized"
	AlipayQRLoginFailed     = "failed"
	AlipayQRLoginExpired    = "expired"
)

type AlipayQRLoginSession struct {
	SessionID    string      `json:"session_id"`
	State        string      `json:"state,omitempty"`
	AuthURL      string      `json:"auth_url,omitempty"`
	Status       string      `json:"status"`
	AccessToken  string      `json:"access_token,omitempty"`
	RefreshToken string      `json:"refresh_token,omitempty"`
	TokenType    string      `json:"token_type,omitempty"`
	ExpiresIn    int64       `json:"expires_in,omitempty"`
	User         *model.User `json:"user,omitempty"`
	Error        string      `json:"error,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

type AlipayQRLoginSessionStore struct {
	mu        sync.RWMutex
	sessions  map[string]*AlipayQRLoginSession
	stateToID map[string]string
}

func NewAlipayQRLoginSessionStore() *AlipayQRLoginSessionStore {
	return &AlipayQRLoginSessionStore{
		sessions:  make(map[string]*AlipayQRLoginSession),
		stateToID: make(map[string]string),
	}
}

func NewAlipayQRLoginSession(ttl time.Duration) *AlipayQRLoginSession {
	now := time.Now()
	sessionID := uuid.NewString()
	return &AlipayQRLoginSession{
		SessionID: sessionID,
		State:     "alipay_qr_" + sessionID,
		Status:    AlipayQRLoginPending,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

func (s *AlipayQRLoginSessionStore) SavePending(session *AlipayQRLoginSession, authURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	copySession := *session
	copySession.AuthURL = authURL
	copySession.Status = AlipayQRLoginPending
	copySession.UpdatedAt = time.Now()
	s.sessions[copySession.SessionID] = &copySession
	s.stateToID[copySession.State] = copySession.SessionID
}

func (s *AlipayQRLoginSessionStore) Get(sessionID string) (*AlipayQRLoginSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}
	s.expireIfNeeded(session)
	copySession := *session
	return &copySession, true
}

func (s *AlipayQRLoginSessionStore) AuthorizeByState(state, accessToken, refreshToken, tokenType string, expiresIn int64, user *model.User) (*AlipayQRLoginSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.getByStateLocked(state)
	if !ok {
		return nil, false
	}
	s.expireIfNeeded(session)
	if session.Status == AlipayQRLoginExpired {
		return nil, false
	}

	session.Status = AlipayQRLoginAuthorized
	session.AccessToken = accessToken
	session.RefreshToken = refreshToken
	session.TokenType = tokenType
	session.ExpiresIn = expiresIn
	session.User = user
	session.Error = ""
	session.UpdatedAt = time.Now()

	copySession := *session
	return &copySession, true
}

func (s *AlipayQRLoginSessionStore) FailByState(state, message string) (*AlipayQRLoginSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.getByStateLocked(state)
	if !ok {
		return nil, false
	}
	s.expireIfNeeded(session)
	if session.Status == AlipayQRLoginExpired {
		return nil, false
	}

	session.Status = AlipayQRLoginFailed
	session.Error = message
	session.UpdatedAt = time.Now()

	copySession := *session
	return &copySession, true
}

func (s *AlipayQRLoginSessionStore) getByStateLocked(state string) (*AlipayQRLoginSession, bool) {
	sessionID, ok := s.stateToID[state]
	if !ok {
		return nil, false
	}
	session, ok := s.sessions[sessionID]
	return session, ok
}

func (s *AlipayQRLoginSessionStore) expireIfNeeded(session *AlipayQRLoginSession) {
	if session.Status == AlipayQRLoginPending && time.Now().After(session.ExpiresAt) {
		session.Status = AlipayQRLoginExpired
		session.UpdatedAt = time.Now()
	}
}
