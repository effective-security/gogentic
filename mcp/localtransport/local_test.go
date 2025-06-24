package localtransport_test

import (
	"context"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/mcp/localtransport"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	transport := localtransport.New()

	assert.NotNil(t, transport)
	assert.NotNil(t, transport.Base)
}

func TestTransport_Start(t *testing.T) {
	transport := localtransport.New()
	ctx := context.Background()

	// Start should always return nil (does nothing in stateless local transport)
	err := transport.Start(ctx)
	assert.NoError(t, err)

	// Test with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = transport.Start(cancelledCtx)
	assert.NoError(t, err)
}

func TestTransport_Close(t *testing.T) {
	t.Run("close with handler", func(t *testing.T) {
		transport := localtransport.New()
		closeCalled := false

		transport.SetCloseHandler(func() {
			closeCalled = true
		})

		err := transport.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("close without handler", func(t *testing.T) {
		transport := localtransport.New()

		err := transport.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		transport := localtransport.New()
		closeCount := 0

		transport.SetCloseHandler(func() {
			closeCount++
		})

		err := transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, closeCount)

		err = transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 2, closeCount)
	})

	t.Run("close with nil handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetCloseHandler(nil)

		err := transport.Close()
		assert.NoError(t, err)
	})
}

func TestTransport_SetCloseHandler(t *testing.T) {
	t.Run("set close handler", func(t *testing.T) {
		transport := localtransport.New()
		handlerCalled := false
		handler := func() {
			handlerCalled = true
		}

		transport.SetCloseHandler(handler)

		// Test the handler by calling Close
		err := transport.Close()
		assert.NoError(t, err)
		assert.True(t, handlerCalled)
	})

	t.Run("set nil close handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetCloseHandler(nil)

		// Should not panic
		assert.NotPanics(t, func() {
			err := transport.Close()
			assert.NoError(t, err)
		})
	})

	t.Run("concurrent close handler setting", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup
		handlerCount := 0
		var mu sync.Mutex

		// Set handlers concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				transport.SetCloseHandler(func() {
					mu.Lock()
					handlerCount++
					mu.Unlock()
				})
			}()
		}

		wg.Wait()

		// Call close to verify handler works
		err := transport.Close()
		assert.NoError(t, err)
		assert.Equal(t, 1, handlerCount)
	})
}

func TestTransport_SetErrorHandler(t *testing.T) {
	t.Run("set error handler", func(t *testing.T) {
		transport := localtransport.New()
		handler := func(err error) {
			// Error handler implementation
		}

		transport.SetErrorHandler(handler)

		// Verify the method doesn't panic
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(handler)
		})
	})

	t.Run("set nil error handler", func(t *testing.T) {
		transport := localtransport.New()
		transport.SetErrorHandler(nil)

		// Should not panic
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(nil)
		})
	})

	t.Run("concurrent error handler setting", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Set handlers concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				handler := func(err error) {
					// Error handler implementation
				}
				transport.SetErrorHandler(handler)
			}()
		}

		wg.Wait()

		// Verify no panics occurred
		assert.NotPanics(t, func() {
			transport.SetErrorHandler(func(err error) {})
		})
	})
}

func TestTransport_Concurrency(t *testing.T) {
	t.Run("concurrent handler operations", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Test concurrent setting of different handlers
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				transport.SetCloseHandler(func() {})
			}
		}()

		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				transport.SetErrorHandler(func(err error) {})
			}
		}()

		wg.Wait()

		// Verify no panics occurred
		assert.NotPanics(t, func() {
			transport.Close()
		})
	})
}

func TestTransport_Integration(t *testing.T) {
	t.Run("full lifecycle", func(t *testing.T) {
		transport := localtransport.New()
		closeCalled := false

		// Set up handlers
		transport.SetCloseHandler(func() {
			closeCalled = true
		})

		transport.SetErrorHandler(func(err error) {
			// Error handler implementation
		})

		// Start the transport
		err := transport.Start(context.Background())
		assert.NoError(t, err)

		// Close the transport
		err = transport.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})
}

func TestTransport_ThreadSafety(t *testing.T) {
	t.Run("thread safety of handler setters", func(t *testing.T) {
		transport := localtransport.New()
		var wg sync.WaitGroup

		// Test that setting handlers from multiple goroutines doesn't cause race conditions
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				transport.SetCloseHandler(func() {})
				transport.SetErrorHandler(func(err error) {})
			}()
		}

		wg.Wait()

		// Verify the transport is still functional
		assert.NotPanics(t, func() {
			transport.Close()
		})
	})
}

func TestTransport_MultipleInstances(t *testing.T) {
	t.Run("multiple transport instances", func(t *testing.T) {
		// Test that multiple transport instances work independently
		transport1 := localtransport.New()
		transport2 := localtransport.New()

		close1Called := false
		close2Called := false

		transport1.SetCloseHandler(func() {
			close1Called = true
		})

		transport2.SetCloseHandler(func() {
			close2Called = true
		})

		// Start both transports
		err1 := transport1.Start(context.Background())
		err2 := transport2.Start(context.Background())
		assert.NoError(t, err1)
		assert.NoError(t, err2)

		// Close both transports
		err1 = transport1.Close()
		err2 = transport2.Close()
		assert.NoError(t, err1)
		assert.NoError(t, err2)

		assert.True(t, close1Called)
		assert.True(t, close2Called)
	})
}

func TestTransport_BaseInheritance(t *testing.T) {
	t.Run("transport inherits from base", func(t *testing.T) {
		transport := localtransport.New()

		// Test that the transport has access to Base methods
		assert.NotNil(t, transport.Base)

		// Test that we can call Base methods through the transport
		// (inheritance test)
		assert.NotPanics(t, func() {
			transport.Close()
		})
	})
}
