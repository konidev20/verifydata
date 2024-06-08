package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
)

type Result struct {
	FolderPath        string          `json:"folder_path"`
	TotalFiles        int             `json:"total_files"`
	IntactFiles       int             `json:"intact_files"`
	CorruptedFiles    int             `json:"corrupted_files"`
	CorruptedFileList []CorruptedFile `json:"corrupted_file_list,omitempty"`
}

type CorruptedFile struct {
	FilePath     string `json:"file_path"`
	ExpectedHash string `json:"expected_hash"`
	ActualHash   string `json:"actual_hash"`
}

type RefCheckOptions struct {
	Path    string
	Exclude []string
	Workers int
	JSON    bool
}

var refCheckOptions RefCheckOptions

func main() {
	var rootCmd = &cobra.Command{
		Use:   "refcheck",
		Short: "refcheck checks the integrity of files in a directory",
		Long: `refcheck is a tool for checking the integrity of files in a directory.
Assuming the file names are the SHA256 hash of the file, it calculates the SHA256 hash of each file and compares it with the file name.
If the file name matches the hash, the file is intact; otherwise, it is corrupted.
The tool can be used to check the integrity of files in a directory before deploying them to a server.`,
		Run: func(cmd *cobra.Command, args []string) {
			runChecker(cmd, refCheckOptions, args)
		},
	}

	rootCmd.Flags().StringVarP(&refCheckOptions.Path, "path", "p", ".", "Path to the folder")
	rootCmd.Flags().StringSliceVarP(&refCheckOptions.Exclude, "exclude", "e", []string{"config", ".DS_Store"}, "Regular expression pattern for excluding files and folders")
	rootCmd.Flags().IntVarP(&refCheckOptions.Workers, "workers", "w", 4, "Number of workers for parallel processing")
	rootCmd.Flags().BoolVarP(&refCheckOptions.JSON, "json", "j", false, "Print the results in JSON format")

	rootCmd.Execute()
}

func runChecker(cmd *cobra.Command, opts RefCheckOptions, _ []string) {
	folderPath := opts.Path
	excludePatterns := opts.Exclude
	numWorkers := opts.Workers
	jsonOutput := opts.JSON

	combinedPattern := "(" + strings.Join(excludePatterns, ")|(") + ")"
	exclude := regexp.MustCompile(combinedPattern)
	result := &Result{FolderPath: folderPath}

	var wg sync.WaitGroup
	fileChan := make(chan string)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				if !exclude.MatchString(filePath) {
					processFile(filePath, result)
				}
			}
		}()
	}

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fileChan <- path
		}
		return nil
	})

	close(fileChan)
	wg.Wait()

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if jsonOutput {
		jsonData, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(jsonData))
	} else {
		tbl := table.New("Result", "Value")
		tbl.WithHeaderSeparatorRow('-')
		tbl.WithPadding(2)
		tbl.WithWriter(cmd.OutOrStdout())
		tbl.AddRow("Total Files", result.TotalFiles)
		tbl.AddRow("Intact Files", result.IntactFiles)
		tbl.AddRow("Corrupted Files", result.CorruptedFiles)
		tbl.Print()

		if result.CorruptedFiles > 0 {
			fmt.Println("\nCorrupted Files:")
			tbl := table.New("File Path", "Expected Hash", "Actual Hash")
			tbl.WithWriter(cmd.OutOrStdout())
			tbl.WithHeaderSeparatorRow('-')
			tbl.WithPadding(2)
			for _, file := range result.CorruptedFileList {
				tbl.AddRow(file.FilePath, file.ExpectedHash, file.ActualHash)
			}
			tbl.Print()
		}
	}
}

func processFile(filePath string, result *Result) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		fmt.Printf("Error calculating SHA256 hash for file %s: %v\n", filePath, err)
		return
	}

	result.TotalFiles++

	expectedHash := filepath.Base(filePath)
	actualHash := hex.EncodeToString(hash.Sum(nil))

	if expectedHash == actualHash {
		result.IntactFiles++
	} else {
		result.CorruptedFiles++
		result.CorruptedFileList = append(result.CorruptedFileList, CorruptedFile{FilePath: filePath, ExpectedHash: expectedHash, ActualHash: actualHash})
	}
}