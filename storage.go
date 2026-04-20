package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketHosts             = []byte("hosts")
	bucketSessions          = []byte("sessions")
	bucketSpeedtestSessions = []byte("speedtest_sessions")
	bucketChatSessions      = []byte("chat_sessions")
)

type HostEntry struct {
	Host     string    `json:"host"`
	LastUsed time.Time `json:"last_used"`
}

type HopResult struct {
	Nr    int     `json:"nr"`
	Host  string  `json:"host"`
	Loss  float64 `json:"loss"`
	Sent  int     `json:"sent"`
	Recv  int     `json:"recv"`
	Best  int64   `json:"best_ms"`
	Avg   int64   `json:"avg_ms"`
	Worst int64   `json:"worst_ms"`
	Last  int64   `json:"last_ms"`
}

type DiagSession struct {
	ID        string      `json:"id"`
	Host      string      `json:"host"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
	Hops      []HopResult `json:"hops"`
}

type Storage struct {
	db *bolt.DB
}

func NewStorage() (*Storage, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "net-monit")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(filepath.Join(dir, "net-monit.db"), 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists(bucketHosts); e != nil {
			return e
		}
		if _, e := tx.CreateBucketIfNotExists(bucketSessions); e != nil {
			return e
		}
		if _, e := tx.CreateBucketIfNotExists(bucketSpeedtestSessions); e != nil {
			return e
		}
		_, e := tx.CreateBucketIfNotExists(bucketChatSessions)
		return e
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) SaveHost(host string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketHosts)

		// collect existing entries
		type kv struct {
			key   []byte
			entry HostEntry
		}
		var entries []kv
		_ = b.ForEach(func(k, v []byte) error {
			var e HostEntry
			if json.Unmarshal(v, &e) == nil {
				entries = append(entries, kv{key: append([]byte{}, k...), entry: e})
			}
			return nil
		})

		// remove existing entry for same host
		for _, kve := range entries {
			if kve.entry.Host == host {
				if err := b.Delete(kve.key); err != nil {
					return err
				}
			}
		}

		// insert/update
		entry := HostEntry{Host: host, LastUsed: time.Now().UTC()}
		val, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		key := []byte(entry.LastUsed.Format(time.RFC3339Nano))
		if err = b.Put(key, val); err != nil {
			return err
		}

		// trim to 20 most recent
		type kve2 struct {
			key      []byte
			lastUsed time.Time
		}
		var all []kve2
		_ = b.ForEach(func(k, v []byte) error {
			var e HostEntry
			if json.Unmarshal(v, &e) == nil {
				all = append(all, kve2{key: append([]byte{}, k...), lastUsed: e.LastUsed})
			}
			return nil
		})
		if len(all) > 20 {
			sort.Slice(all, func(i, j int) bool {
				return all[i].lastUsed.Before(all[j].lastUsed)
			})
			for _, old := range all[:len(all)-20] {
				if err := b.Delete(old.key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Storage) GetHosts() ([]string, error) {
	var entries []HostEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketHosts)
		return b.ForEach(func(_, v []byte) error {
			var e HostEntry
			if json.Unmarshal(v, &e) == nil {
				entries = append(entries, e)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastUsed.After(entries[j].LastUsed)
	})
	hosts := make([]string, len(entries))
	for i, e := range entries {
		hosts[i] = e.Host
	}
	return hosts, nil
}

func (s *Storage) DeleteHost(host string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketHosts)
		return b.ForEach(func(k, v []byte) error {
			var e HostEntry
			if json.Unmarshal(v, &e) == nil && e.Host == host {
				return b.Delete(k)
			}
			return nil
		})
	})
}

func (s *Storage) SaveSession(session DiagSession) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		val, err := json.Marshal(session)
		if err != nil {
			return err
		}
		key := []byte(session.StartedAt.UTC().Format(time.RFC3339Nano))
		return b.Put(key, val)
	})
}

func (s *Storage) GetSessions() ([]DiagSession, error) {
	var sessions []DiagSession
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		return b.ForEach(func(_, v []byte) error {
			var s DiagSession
			if json.Unmarshal(v, &s) == nil {
				sessions = append(sessions, s)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

// ── Speedtest Storage ─────────────────────────────────────────────────────────

type SpeedtestSession struct {
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	Download   float64   `json:"download_mbps"`
	Upload     float64   `json:"upload_mbps"`
	Ping       int64     `json:"ping_ms"`
	Jitter     float64   `json:"jitter_ms"`
	Loss       float64   `json:"loss_pct"`
	Server     string    `json:"server"`
	Failed     bool      `json:"failed"`
	FailReason string    `json:"fail_reason,omitempty"`
}

func (s *Storage) SaveSpeedtestSession(session SpeedtestSession) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSpeedtestSessions)
		val, err := json.Marshal(session)
		if err != nil {
			return err
		}
		key := []byte(session.StartedAt.UTC().Format(time.RFC3339Nano))
		if err = b.Put(key, val); err != nil {
			return err
		}
		// trim to 50 most recent
		type kve struct {
			key       []byte
			startedAt time.Time
		}
		var all []kve
		_ = b.ForEach(func(k, v []byte) error {
			var sess SpeedtestSession
			if json.Unmarshal(v, &sess) == nil {
				all = append(all, kve{key: append([]byte{}, k...), startedAt: sess.StartedAt})
			}
			return nil
		})
		if len(all) > 50 {
			sort.Slice(all, func(i, j int) bool {
				return all[i].startedAt.Before(all[j].startedAt)
			})
			for _, old := range all[:len(all)-50] {
				if err := b.Delete(old.key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Storage) GetSpeedtestSessions() ([]SpeedtestSession, error) {
	var sessions []SpeedtestSession
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSpeedtestSessions)
		return b.ForEach(func(_, v []byte) error {
			var sess SpeedtestSession
			if json.Unmarshal(v, &sess) == nil {
				sessions = append(sessions, sess)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

// ── Chat Storage ──────────────────────────────────────────────────────────────

type ChatRole string

const (
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
	RoleSystem    ChatRole = "system"
)

type ChatMessage struct {
	ID        string    `json:"id"`
	Role      ChatRole  `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type ChatSession struct {
	ID        string        `json:"id"`
	Messages  []ChatMessage `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (s *Storage) SaveChatSession(session ChatSession) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketChatSessions)
		val, err := json.Marshal(session)
		if err != nil {
			return err
		}
		key := []byte(session.CreatedAt.UTC().Format(time.RFC3339Nano) + "_" + session.ID)
		if err = b.Put(key, val); err != nil {
			return err
		}
		// trim to 100 most recent
		type kve struct {
			key       []byte
			createdAt time.Time
		}
		var all []kve
		_ = b.ForEach(func(k, v []byte) error {
			var sess ChatSession
			if json.Unmarshal(v, &sess) == nil {
				all = append(all, kve{key: append([]byte{}, k...), createdAt: sess.CreatedAt})
			}
			return nil
		})
		if len(all) > 100 {
			sort.Slice(all, func(i, j int) bool {
				return all[i].createdAt.Before(all[j].createdAt)
			})
			for _, old := range all[:len(all)-100] {
				if err := b.Delete(old.key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Storage) GetChatSessions() ([]ChatSession, error) {
	var sessions []ChatSession
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketChatSessions)
		return b.ForEach(func(_, v []byte) error {
			var sess ChatSession
			if json.Unmarshal(v, &sess) == nil {
				sessions = append(sessions, sess)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

func (s *Storage) GetChatSession(id string) (*ChatSession, error) {
	var result *ChatSession
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketChatSessions)
		return b.ForEach(func(_, v []byte) error {
			var sess ChatSession
			if json.Unmarshal(v, &sess) == nil && sess.ID == id {
				result = &sess
			}
			return nil
		})
	})
	return result, err
}

func (s *Storage) DeleteChatSession(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketChatSessions)
		return b.ForEach(func(k, v []byte) error {
			var sess ChatSession
			if json.Unmarshal(v, &sess) == nil && sess.ID == id {
				return b.Delete(k)
			}
			return nil
		})
	})
}
