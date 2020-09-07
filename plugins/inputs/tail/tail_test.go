package tail

import (
	"io/ioutil"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/parsers"
	"github.com/influxdata/telegraf/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailFromBeginning(t *testing.T) {
	if os.Getenv("CIRCLE_PROJECT_REPONAME") != "" {
		t.Skip("Skipping CI testing due to race conditions")
	}

	tmpfile, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.WriteString("cpu,mytag=foo usage_idle=100\n")
	require.NoError(t, err)

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{tmpfile.Name()}
	tt.SetParserFunc(parsers.NewInfluxParser)
	defer tt.Stop()
	defer tmpfile.Close()

	acc := testutil.Accumulator{}
	require.NoError(t, tt.Start(&acc))
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		},
		map[string]string{
			"mytag": "foo",
			"path":  tmpfile.Name(),
		})
}

func TestTailFromEnd(t *testing.T) {
	if os.Getenv("CIRCLE_PROJECT_REPONAME") != "" {
		t.Skip("Skipping CI testing due to race conditions")
	}

	tmpfile, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.WriteString("cpu,mytag=foo usage_idle=100\n")
	require.NoError(t, err)

	tt := NewTail()
	tt.Files = []string{tmpfile.Name()}
	tt.SetParserFunc(parsers.NewInfluxParser)
	defer tt.Stop()
	defer tmpfile.Close()

	acc := testutil.Accumulator{}
	require.NoError(t, tt.Start(&acc))
	for _, tailer := range tt.tailers {
		for n, err := tailer.Tell(); err == nil && n == 0; n, err = tailer.Tell() {
			// wait for tailer to jump to end
			runtime.Gosched()
		}
	}

	_, err = tmpfile.WriteString("cpu,othertag=foo usage_idle=100\n")
	require.NoError(t, err)
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		},
		map[string]string{
			"othertag": "foo",
			"path":     tmpfile.Name(),
		})
	assert.Len(t, acc.Metrics, 1)
}

func TestTailBadLine(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{tmpfile.Name()}
	tt.SetParserFunc(parsers.NewInfluxParser)
	defer tt.Stop()
	defer tmpfile.Close()

	acc := testutil.Accumulator{}
	require.NoError(t, tt.Start(&acc))
	require.NoError(t, acc.GatherError(tt.Gather))

	_, err = tmpfile.WriteString("cpu mytag= foo usage_idle= 100\n")
	require.NoError(t, err)

	acc.WaitError(1)
	assert.Contains(t, acc.Errors[0].Error(), "E! Malformed log line")
}

func TestTailDosLineendings(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.WriteString("cpu usage_idle=100\r\ncpu2 usage_idle=200\r\n")
	require.NoError(t, err)

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{tmpfile.Name()}
	tt.SetParserFunc(parsers.NewInfluxParser)
	defer tt.Stop()
	defer tmpfile.Close()

	acc := testutil.Accumulator{}
	require.NoError(t, tt.Start(&acc))
	require.NoError(t, acc.GatherError(tt.Gather))

	acc.Wait(2)
	acc.AssertContainsFields(t, "cpu",
		map[string]interface{}{
			"usage_idle": float64(100),
		})
	acc.AssertContainsFields(t, "cpu2",
		map[string]interface{}{
			"usage_idle": float64(200),
		})
}

func TestGrokParseLogFilesWithMultiline(t *testing.T) {
	thisdir := getCurrentDir()
	//we make sure the timeout won't kick in
	duration, _ := time.ParseDuration("100s")

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{thisdir + "testdata/test_multiline.log"}
	tt.MultilineConfig = MultilineConfig{
		Pattern: `^[^\[]`,
		What:    Previous,
		Negate:  false,
		Timeout: &internal.Duration{Duration: duration},
	}
	tt.SetParserFunc(createGrokParser)

	acc := testutil.Accumulator{}
	assert.NoError(t, tt.Start(&acc))
	acc.Wait(3)

	expectedPath := thisdir + "testdata/test_multiline.log"
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is debug",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "DEBUG",
		})
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is info",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "INFO",
		})
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: Sorry, something wrong! java.lang.ArithmeticException: / by zero\tat com.foo.HelloExample2.divide(HelloExample2.java:24)\tat com.foo.HelloExample2.main(HelloExample2.java:14)",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "ERROR",
		})

	assert.Equal(t, uint64(3), acc.NMetrics())
	tt.Stop()
}

func TestGrokParseLogFilesWithMultilineTimeout(t *testing.T) {
	thisdir := getCurrentDir()
	// set tight timeout for tests
	duration, _ := time.ParseDuration("10ms")

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{thisdir + "testdata/test_multiline.log"}
	tt.MultilineConfig = MultilineConfig{
		Pattern: `^[^\[]`,
		What:    Previous,
		Negate:  false,
		Timeout: &internal.Duration{Duration: duration},
	}
	tt.SetParserFunc(createGrokParser)

	acc := testutil.Accumulator{}
	assert.NoError(t, tt.Start(&acc))
	acc.Wait(4)

	assert.Equal(t, uint64(4), acc.NMetrics())
	expectedPath := thisdir + "testdata/test_multiline.log"
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is warn",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "WARN",
		})

	tt.Stop()
}

func TestGrokParseLogFilesWithMultilineTailerCloseFlushesMultilineBuffer(t *testing.T) {
	thisdir := getCurrentDir()
	//we make sure the timeout won't kick in
	duration, _ := time.ParseDuration("100s")

	tt := NewTail()
	tt.FromBeginning = true
	tt.Files = []string{thisdir + "testdata/test_multiline.log"}
	tt.MultilineConfig = MultilineConfig{
		Pattern: `^[^\[]`,
		What:    Previous,
		Negate:  false,
		Timeout: &internal.Duration{Duration: duration},
	}
	tt.SetParserFunc(createGrokParser)

	acc := testutil.Accumulator{}
	assert.NoError(t, tt.Start(&acc))
	acc.Wait(3)
	assert.Equal(t, uint64(3), acc.NMetrics())
	// Close tailer, so multiline buffer is flushed
	tt.Stop()
	acc.Wait(4)

	expectedPath := thisdir + "testdata/test_multiline.log"
	acc.AssertContainsTaggedFields(t, "tail_grok",
		map[string]interface{}{
			"message": "HelloExample: This is warn",
		},
		map[string]string{
			"path":     expectedPath,
			"loglevel": "WARN",
		})
}

func createGrokParser() (parsers.Parser, error) {
	grokConfig := &parsers.Config{
		MetricName:             "tail_grok",
		GrokPatterns:           []string{"%{TEST_LOG_MULTILINE}"},
		GrokCustomPatternFiles: []string{getCurrentDir() + "testdata/test-patterns"},
		DataFormat:             "grok",
	}
	parser, err := parsers.NewParser(grokConfig)
	return parser, err
}

func getCurrentDir() string {
	_, filename, _, _ := runtime.Caller(1)
	return strings.Replace(filename, "tail_test.go", "", 1)
}
