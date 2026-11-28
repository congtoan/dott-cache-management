package pool

import "testing"

func open(t *testing.T) *Pool {
	p, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestMergeAndPriority(t *testing.T) {
	p := open(t)
	// hàng loạt trước
	p.Add([]Item{{Path: "/a/", Action: "refresh", Prio: PrioBulk, Ord: 2}, {Path: "/b/", Action: "refresh", Prio: PrioBulk, Ord: 1}})
	// sự kiện cho /a/ → nâng lên mức 1
	if a, m, _ := p.Add([]Item{{Path: "/a/", Action: "refresh", Prio: PrioEvent}}); a != 0 || m != 1 {
		t.Fatalf("gộp: added %d merged %d", a, m)
	}
	act, items, _ := p.Take(10, 20, 40)
	if act != "refresh" || len(items) != 1 || items[0].Path != "/a/" || items[0].Prio != PrioEvent {
		t.Fatalf("sự kiện phải đi trước hàng loạt: %v %+v", act, items)
	}
	// hàng loạt không hạ ưu tiên của dòng đang có
	p.Add([]Item{{Path: "/c/", Action: "refresh", Prio: PrioRelated}})
	p.Add([]Item{{Path: "/c/", Action: "refresh", Prio: PrioBulk}})
	if pr, _ := p.HeadPrio(); pr != PrioRelated {
		t.Fatalf("head prio %d, muốn %d", pr, PrioRelated)
	}
	// gỡ luôn đi trước
	p.Add([]Item{{Path: "/d/", Action: "remove", Prio: PrioEvent}})
	act, items, _ = p.Take(10, 20, 40)
	if act != "remove" || len(items) != 1 || items[0].Prio != PrioRemove {
		t.Fatalf("gỡ phải đi trước: %v %+v", act, items)
	}
	// refresh hàng loạt không xóa được việc gỡ đang chờ
	p.Add([]Item{{Path: "/e/", Action: "remove"}})
	p.Add([]Item{{Path: "/e/", Action: "refresh", Prio: PrioBulk}})
	if _, a, _ := p.Current("/e/"); a != "remove" {
		t.Fatalf("/e/ phải vẫn là remove, có %s", a)
	}
	// nhưng sự kiện đăng lại bài thì thắng gỡ
	p.Add([]Item{{Path: "/e/", Action: "refresh", Prio: PrioEvent}})
	if _, a, _ := p.Current("/e/"); a != "refresh" {
		t.Fatalf("/e/ phải là refresh sau sự kiện, có %s", a)
	}
}

func TestGenerationAndRestart(t *testing.T) {
	dir := t.TempDir()
	p, _ := Open(dir)
	p.Add([]Item{{Path: "/x/", Action: "refresh", Prio: PrioEvent}})
	_, items, _ := p.Take(10, 20, 40)
	// trong lúc chạy, bài bị gỡ
	p.Add([]Item{{Path: "/x/", Action: "remove"}})
	g, a, _ := p.Current("/x/")
	if g == items[0].Gen || a != "remove" {
		t.Fatalf("gen phải tăng và action = remove: gen %d/%d %s", g, items[0].Gen, a)
	}
	if ok, _ := p.Done(items[0]); ok {
		t.Fatal("Done với gen cũ không được xóa dòng")
	}
	act, items2, _ := p.Take(10, 20, 40)
	if act != "remove" || len(items2) != 1 {
		t.Fatalf("việc gỡ phải còn trong hàng đợi: %v %+v", act, items2)
	}
	// restart khi đang running: trở về queued
	p.Close()
	p, _ = Open(dir)
	defer p.Close()
	if _, items3, _ := p.Take(10, 20, 40); len(items3) != 1 {
		t.Fatalf("sau restart việc running phải quay lại hàng đợi: %+v", items3)
	}
}

func TestFailBackoffAndDrop(t *testing.T) {
	p := open(t)
	p.Add([]Item{{Path: "/f/", Action: "refresh", Prio: PrioEvent}})
	for i := 1; i <= MaxTries; i++ {
		// cho tới giờ thử lại ngay
		p.db.Exec(`UPDATE pool SET next_at=0`)
		_, items, _ := p.Take(10, 20, 40)
		if len(items) != 1 {
			t.Fatalf("lần %d: không lấy được việc", i)
		}
		dropped, _ := p.Fail(items[0], "lỗi thử")
		if dropped != (i == MaxTries) {
			t.Fatalf("lần %d: dropped=%v", i, dropped)
		}
		if i == 1 {
			if _, items, _ := p.Take(10, 20, 40); len(items) != 0 {
				t.Fatal("việc lỗi chưa tới giờ thử lại không được lấy")
			}
		}
	}
	if _, _, ok := p.Current("/f/"); ok {
		t.Fatal("quá MaxTries phải bị bỏ")
	}
}

func TestBatchSizes(t *testing.T) {
	p := open(t)
	var its []Item
	for i := 0; i < 100; i++ {
		its = append(its, Item{Path: "/bulk/" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "/", Action: "refresh", Prio: PrioBulk, Ord: i})
	}
	its = append(its, Item{Path: "/ev1/", Action: "refresh", Prio: PrioEvent}, Item{Path: "/ev2/", Action: "refresh", Prio: PrioRelated})
	p.Add(its)
	_, items, _ := p.Take(500, 20, 40)
	if len(items) != 2 {
		t.Fatalf("lô sự kiện chỉ gồm việc mức <= 2: %d", len(items))
	}
	_, items, _ = p.Take(500, 20, 40)
	if len(items) != 40 || items[0].Ord != 0 {
		t.Fatalf("lô hàng loạt 40 việc theo ord: %d, ord đầu %d", len(items), items[0].Ord)
	}
}
