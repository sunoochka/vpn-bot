package device

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"syscall"
	"time"

	"vpn-bot/internal/logging"
)

var logEntryRe = regexp.MustCompile(
	`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})(?:\.\d+)?\s+(?:from\s+)?([0-9.]+):(\d+).*email:\s*([a-f0-9-]+)`,
)

// LogParser tails an Xray access log and emits connection events.
// It handles log rotation by detecting inode changes.
// It tolerates malformed lines and does not block on errors.
type LogParser struct {
	cfg    Config
	logger *logging.Logger
	out    chan<- ConnectionEvent
}

func NewLogParser(cfg Config, logger *logging.Logger, out chan<- ConnectionEvent) *LogParser {
	return &LogParser{cfg: cfg, logger: logger, out: out}
}

func (p *LogParser) Run(ctx context.Context) {
	if !p.cfg.Enabled {
		p.logger.Info("device_tracking_disabled", "device tracking disabled (no access log path)", nil)
		return
	}

	for {
		if err := p.runOnce(ctx); err != nil {
			p.logger.Error("log_parser_error", "log parser error", map[string]interface{}{"error": err.Error()})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
			// retry on error
		}
	}
}

func (p *LogParser) runOnce(ctx context.Context) error {
	f, err := os.Open(p.cfg.AccessLogPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}
	inode := getInode(stat)

	// start at end of file so we only process new entries.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	r := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if rotated, newInode, reopenErr := p.checkRotation(inode); reopenErr == nil && rotated {
					inode = newInode
					f.Close()
					f, err = os.Open(p.cfg.AccessLogPath)
					if err != nil {
						return err
					}
					r = bufio.NewReader(f)
					continue
				}

				// no data yet
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return err
		}

		if ev, ok := parseLogLine(line); ok {
			select {
			case p.out <- ev:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (p *LogParser) checkRotation(prevInode uint64) (rotated bool, newInode uint64, err error) {
	st, err := os.Stat(p.cfg.AccessLogPath)
	if err != nil {
		return false, 0, err
	}
	newInode = getInode(st)
	return newInode != prevInode, newInode, nil
}

func getInode(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}

func parseLogLine(line string) (ConnectionEvent, bool) {
	m := logEntryRe.FindStringSubmatch(line)
	if m == nil {
		return ConnectionEvent{}, false
	}
	layout := "2006/01/02 15:04:05"
	t, err := time.Parse(layout, m[1])
	if err != nil {
		return ConnectionEvent{}, false
	}

	ip := m[2]
	port := 0
	fmt.Sscanf(m[3], "%d", &port)
	uuid := m[4]
	if uuid == "" {
		return ConnectionEvent{}, false
	}

	return ConnectionEvent{Timestamp: t, UUID: uuid, IP: ip, Port: port}, true
}
