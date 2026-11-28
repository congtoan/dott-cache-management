package main

// generate: phần dùng chung của "update": tạo cache cho danh sách path theo lô, có điều tiết theo load,
// upload song song với lô kế tiếp, URL prefork lỗi thì thử lại bằng HTTP.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/congtoan/dott-cache-management/internal/kv"
	"github.com/congtoan/dott-cache-management/internal/s3"
)

type genStats struct {
	prefOK, retry int
	tUpload       time.Duration
	waits         int           // số lần nghỉ trước lô (Go)
	waited        time.Duration // tổng thời gian nghỉ trước lô
	phpPauses     int           // số lần nghỉ trong lô (PHP)
	phpWaited     int           // giây
	maxLoad       float64       // load cao nhất đo được trước các lô
}

// remoteLoad đọc load 1 phút của VPS chính (dott-prefork <domain> --loadavg).
func remoteLoad(ctx context.Context, cfg Config) (float64, error) {
	out, err := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", cfg.Renew.SSHHost, cfg.Renew.Command+" --loadavg").Output()
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(out))
	if len(f) < 2 || f[0] != "LOAD" {
		return 0, fmt.Errorf("trả về lạ: %q", out)
	}
	return strconv.ParseFloat(f[1], 64)
}

// waitLoad nghỉ trước một lô nếu load > max_load, tới khi load <= resume_load (tối đa max_wait_sec).
func waitLoad(ctx context.Context, cfg Config, st *genStats, label string) {
	th := cfg.Throttle.withDefaults()
	l, err := remoteLoad(ctx, cfg)
	if err != nil {
		fmt.Printf("    (không đọc được load: %v)\n", err)
		return
	}
	st.maxLoad = max(st.maxLoad, l)
	if l <= th.MaxLoad {
		return
	}
	t0 := time.Now()
	st.waits++
	for l > th.ResumeLoad && time.Since(t0) < time.Duration(th.MaxWaitSec)*time.Second && ctx.Err() == nil {
		time.Sleep(5 * time.Second)
		if l, err = remoteLoad(ctx, cfg); err != nil {
			break
		}
	}
	st.waited += time.Since(t0)
	fmt.Printf("[%s] load %.2f > %.2f: nghỉ %.0f s, load hiện tại %.2f\n", label, st.maxLoad, th.MaxLoad, time.Since(t0).Seconds(), l)
}

// generate tạo cache cho paths (đã sắp theo thứ tự ưu tiên) theo lô, upload lên mọi đích.
func generate(ctx context.Context, cfg Config, clients []*s3.Client, paths []string, batch int, col *collector, label string) genStats {
	var st genStats
	th := cfg.Throttle.withDefaults()
	cmd := cfg.Renew.Command + fmt.Sprintf(" --max-load=%.2f --resume-load=%.2f", th.MaxLoad, th.ResumeLoad)
	upQ := make(chan []string, 2)
	upDone := make(chan struct{})
	go func() {
		for b := range upQ {
			t0 := time.Now()
			runUpload(ctx, cfg, b, []string{"index-https.html"}, clients, col.ch)
			st.tUpload += time.Since(t0)
		}
		close(upDone)
	}()
	nb := (len(paths) + batch - 1) / batch
	for i := 0; i < len(paths); i += batch {
		if ctx.Err() != nil {
			break
		}
		if i > 0 && th.PauseSec > 0 {
			time.Sleep(time.Duration(th.PauseSec) * time.Second)
		}
		waitLoad(ctx, cfg, &st, label)
		b := paths[i:min(i+batch, len(paths))]
		t0 := time.Now()
		thrPauses, thrWaited = 0, 0
		ok, failed, err := runRemoteQuiet(ctx, cfg.Renew.SSHHost, cmd, b, col.ch)
		if err != nil {
			fmt.Printf("[%s] lô %d/%d: LỖI %v\n", label, i/batch+1, nb, err)
		}
		var retry []string
		for p, e := range failed {
			if retryableByHTTP(e) {
				retry = append(retry, p)
			}
		}
		sort.Strings(retry)
		st.prefOK += len(ok)
		st.retry += len(retry)
		thr := ""
		st.phpPauses += thrPauses
		st.phpWaited += thrWaited
		if thrPauses > 0 {
			thr = fmt.Sprintf(", PHP nghỉ %d lần (%d s) vì load", thrPauses, thrWaited)
		}
		fmt.Printf("[%s] lô %d/%d: prefork %d/%d ok, %d thử lại bằng HTTP, %.1f s (%.1f URL/s)%s\n",
			label, i/batch+1, nb, len(ok), len(b), len(retry), time.Since(t0).Seconds(), float64(len(ok))/time.Since(t0).Seconds(), thr)
		upQ <- append(ok, retry...)
	}
	close(upQ)
	<-upDone
	return st
}

// kvEnsure: sau khi upload, thêm key KV cho path đã lên S3 đủ mọi đích mà KV chưa có (thứ tự an toàn:
// upload trước, PUT key sau). Chỉ PUT key còn thiếu (list trước rồi so), vì mỗi key ghi vào KV đều
// tính một lượt ghi (gói miễn phí của Cloudflare chỉ có 1.000 lượt/ngày).
func kvEnsure(ctx context.Context, c *kv.Client, cfg Config, paths []string, all []result, nTargets int, label string) {
	bad := map[string]bool{}
	for _, p := range failedPaths(paths, all, nTargets) {
		bad[p] = true
	}
	have, err := c.ListKeys(ctx, "")
	if err != nil {
		fmt.Printf("[%s] KV: list lỗi %v, bỏ qua\n", label, err)
		return
	}
	hs := map[string]bool{}
	for _, k := range have {
		hs[k] = true
	}
	var put []string
	for _, p := range paths {
		if k := kv.Key(p); !bad[p] && !hs[k] {
			put = append(put, k)
			hs[k] = true
		}
	}
	if len(put) == 0 {
		fmt.Printf("[%s] KV: đã đủ key (%d)\n", label, len(have))
		return
	}
	val := cfg.KV.Value
	if val == "" {
		val = "1"
	}
	failed, err := c.PutBulk(ctx, put, val)
	fmt.Printf("[%s] KV: thêm %d key còn thiếu, lỗi %d %s\n", label, len(put), len(failed), errStr(err))
}

// thrPauses/thrWaited: cộng dồn các dòng "THROTTLE {json}" (mỗi blog một dòng) trong một lần runRemote.
var thrPauses, thrWaited int

func addThrottle(js string) {
	var t struct {
		Pauses int `json:"pauses"`
		Waited int `json:"waited"`
	}
	if json.Unmarshal([]byte(js), &t) == nil {
		thrPauses += t.Pauses
		thrWaited += t.Waited
	}
}
