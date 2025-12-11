package internal

import (
	"bufio"
	"embed"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed testdata/test.txt
var testFS embed.FS

func TestMustTmpFileFromEmbedFS(t *testing.T) {
	result := MustTmpFileFromEmbedFS(testFS, "testdata/test.txt")
	file, err := os.Open(result)
	require.NoError(t, err)
	scanner := bufio.NewScanner(file)
	require.True(t, scanner.Scan())
	require.Equal(t, scanner.Text(), "hello openmcp!")
	os.RemoveAll(result)
}
