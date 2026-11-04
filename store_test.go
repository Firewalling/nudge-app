package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentConfigTransactions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			unlock, err := lockConfig()
			if err != nil {
				errs <- err
				return
			}
			defer unlock()
			c, p, err := load()
			if err == nil {
				c.Reminders = append(c.Reminders, Reminder{Kind: "every", Every: "1h", Message: fmt.Sprint(i)})
				err = save(c, p)
			}
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	c, _, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reminders) != 16 {
		t.Fatalf("lost concurrent update: got %d", len(c.Reminders))
	}
}
