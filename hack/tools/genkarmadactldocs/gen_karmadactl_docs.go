package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/karmada-io/karmada/pkg/karmadactl"
	"github.com/karmada-io/karmada/pkg/karmadactl/util"
	"github.com/karmada-io/karmada/pkg/util/lifted"
)

// PrintCLIByTag print custom defined index
func PrintCLIByTag(cmd *cobra.Command, all []*cobra.Command, tag string) string {
	var result string
	var pl []string
	for _, c := range all {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if val, ok := c.Annotations[util.TagCommandGroup]; !ok || val != tag {
			continue
		}
		cname := cmd.Name() + " " + c.Name()
		link := cname
		link = strings.Replace(link, " ", "_", -1) + ".md"
		pl = append(pl, fmt.Sprintf("* [%s](%s)\t - %s\n", cname, link, c.Long))
	}

	for _, v := range pl {
		result += v
	}
	result += "\n"
	return result
}

// GenMarkdownTreeForIndex generate the index page for karmadactl
func GenMarkdownTreeForIndex(cmd *cobra.Command, dir string) error {
	basename := strings.Replace(cmd.CommandPath(), " ", "_", -1) + "_index" + ".md"
	filename := filepath.Join(dir, basename)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = io.WriteString(f, "---\ntitle: Karmadactl Commands\n---\n\n\n"); err != nil {
		return err
	}

	for _, tp := range []string{util.GroupBasic, util.GroupClusterRegistration, util.GroupClusterManagement, util.GroupClusterTroubleshootingAndDebugging, util.GroupAdvancedCommands} {
		// write header of type
		_, err = io.WriteString(f, "## "+tp+"\n\n")
		if err != nil {
			return err
		}
		str := PrintCLIByTag(cmd, cmd.Commands(), tp)
		// write header of type
		_, err = io.WriteString(f, str)
		if err != nil {
			return err
		}
	}

	_, err = io.WriteString(f, "###### Auto generated by [script in Karmada](https://github.com/karmada-io/karmada/tree/master/hack/tools/genkarmadactldocs).")
	if err != nil {
		return err
	}
	return nil
}

func main() {
	// use os.Args instead of "flags" because "flags" will mess up the man pages!
	path := ""
	if len(os.Args) == 2 {
		path = os.Args[1]
	} else if len(os.Args) > 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [output directory]\n", os.Args[0])
		os.Exit(1)
	}

	outDir, err := lifted.OutDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get output directory: %v\n", err)
		os.Exit(1)
	}

	// Set environment variables used by karmadactl so the output is consistent,
	// regardless of where we run.
	os.Setenv("HOME", "/home/username")
	karmadactl := karmadactl.NewKarmadaCtlCommand("karmadactl", "karmadactl")
	karmadactl.DisableAutoGenTag = true
	err = doc.GenMarkdownTree(karmadactl, outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate docs: %v\n", err)
		os.Exit(1)
	}

	err = GenMarkdownTreeForIndex(karmadactl, outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate index docs: %v\n", err)
		os.Exit(1)
	}

	err = filepath.Walk(outDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) < 1 {
			return nil
		}

		firstL := lines[0]
		if !strings.HasPrefix(firstL, "## karmadactl") {
			return nil
		}

		lines[len(lines)-1] = "#### Go Back to [Karmadactl Commands](karmadactl_index.md) Homepage.\n\n\n###### Auto generated by [spf13/cobra script in Karmada](https://github.com/karmada-io/karmada/tree/master/hack/tools/genkarmadactldocs)."

		title := strings.TrimPrefix(firstL, "## ")
		lines[0] = "---"
		newlines := []string{"---", "title: " + title}

		newlines = append(newlines, lines...)
		newcontent := strings.Join(newlines, "\n")
		return os.WriteFile(path, []byte(newcontent), info.Mode())
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to process docs: %v\n", err)
		os.Exit(1)
	}
}
