package mark

import (
	"io"
	"regexp"
	"strings"

	"github.com/btamayo/mark/pkg/mark/stdlib"
	bf "github.com/kovetskiy/blackfriday/v2"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

type ConfluenceRenderer struct {
	bf.Renderer

	Stdlib *stdlib.Lib
}

func splitExceptOnQuotes(s string) []string {
	a := []string{}
	sb := &strings.Builder{}
	quoted := false
	for _, r := range s {
		if r == '"' {
			quoted = !quoted
			sb.WriteRune(r) // keep '"' otherwise comment this line
		} else if !quoted && r == ' ' {
			a = append(a, sb.String())
			sb.Reset()
		} else {
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		a = append(a, sb.String())
	}

	return a
}

// ParseLanguage will parse the info string (https://github.github.com/gfm/#info-string)
// and return the language (the first word)
func ParseLanguage(info string) string {
	// info takes the following form: language? [collapse] [title="<any string>"]?
	// let's split it by spaces
	paramlist := strings.Fields(info)

	// get the word in question, aka the first one
	first := info
	if len(paramlist) > 0 {
		first = paramlist[0]
	}

	if first == "collapse" || strings.HasPrefix(first, "title=") || strings.HasPrefix(first, "theme=") {
		// collapsing or including a title without a language
		return ""
	}

	// the default case with language being the first one
	return first
}

func ParseTheme(info string) string {
	// let's split it by spaces
	paramlist := splitExceptOnQuotes(info)
	var title string

	// find something that starts with title=
	for _, param := range paramlist {
		log.Infof(nil, "Checking theme: %s", param)

		if strings.HasPrefix(param, "theme") {
			if strings.HasPrefix(param, "theme=") {
				// drop the title=
				title = strings.TrimPrefix(param, "theme=")

				// Get rid of quotes and trim whitespace
				title = title[1 : len(title)-1]
				title = strings.TrimSpace(title)

				log.Info("Found theme: %s", param)
				return title
			} else {
				// Be nice to the developer
				log.Debugf(karma.Describe("info", info), "Found string `theme` in info, but not in the correct format, set theme for a code block using: theme=\"Eclipse\". See https://confluence.atlassian.com/doc/code-block-macro-139390.html")
			}
		}

	}

	return ""
}

func ParseTitle(info string) string {
	// let's split it by spaces
	paramlist := splitExceptOnQuotes(info)
	var title string

	// find something that starts with title=
	for _, param := range paramlist {
		log.Infof(nil, "Checking title: %s", param)

		if strings.HasPrefix(param, "title") {
			if strings.HasPrefix(param, "title=") {
				// drop the title=
				title = strings.TrimPrefix(param, "title=")

				// Get rid of quotes and trim whitespace
				title = title[1 : len(title)-1]
				title = strings.TrimSpace(title)

				log.Infof(nil, "Found title: %s", title)
				return title
			} else {
				// Be nice to the developer
				log.Debugf(karma.Describe("info", info), "Found string `title` in info, but not in the correct format, set title for a code block using: title=\"My Title Here\"")
			}
		}
	}

	return ""
}

func (renderer ConfluenceRenderer) RenderNode(
	writer io.Writer,
	node *bf.Node, // Markdown node
	entering bool,
) bf.WalkStatus {
	// If it's a codeblock, parse the "info" string: https://github.github.com/gfm/#info-string
	if node.Type == bf.CodeBlock {
		infoString := string(node.Info)
		curr := karma.Describe("RenderNode", infoString)
		log.Tracef(curr, "RenderNode")

		// https://stackoverflow.com/questions/36209677/how-can-i-conditionally-set-a-variable-in-a-go-template-based-on-an-expression-w
		// ^^^ way too much work to avoid some inelegant code

		renderer.Stdlib.Templates.ExecuteTemplate(
			writer,
			"ac:code",
			struct {
				Language string
				Collapse bool
				Theme    string
				Title    string
				Text     string
			}{
				// todo(btamayo): note – currently, this is done by passing any info string to an extractor
				//       		  maybe we can optimize later to parse the string once?
				ParseLanguage(infoString),
				strings.Contains(infoString, "collapse"),
				ParseTheme(infoString),
				ParseTitle(infoString),
				strings.TrimSuffix(string(node.Literal), "\n"),
			},
		)

		return bf.GoToNext
	}
	return renderer.Renderer.RenderNode(writer, node, entering)
}

// compileMarkdown will replace tags like <ac:rich-tech-body> with escaped
// equivalent, because bf markdown parser replaces that tags with
// <a href="ac:rich-text-body">ac:rich-text-body</a> for whatever reason.
func CompileMarkdown(
	markdown []byte,
	stdlib *stdlib.Lib,
) string {
	// log.Tracef(nil, "rendering markdown:\n%s", string(markdown))

	colon := regexp.MustCompile(`---bf-COLON---`)

	tags := regexp.MustCompile(`<(/?\S+?):(\S+?)>`)

	markdown = tags.ReplaceAll(
		markdown,
		[]byte(`<$1`+colon.String()+`$2>`),
	)

	renderer := ConfluenceRenderer{
		Renderer: bf.NewHTMLRenderer(
			bf.HTMLRendererParameters{
				Flags: bf.UseXHTML |
					bf.Smartypants |
					bf.SmartypantsFractions |
					bf.SmartypantsDashes |
					bf.SmartypantsLatexDashes,
			},
		),

		Stdlib: stdlib,
	}

	html := bf.Run(
		markdown,
		bf.WithRenderer(renderer),
		bf.WithExtensions(
			bf.NoIntraEmphasis|
				bf.Tables|
				bf.FencedCode|
				bf.Autolink|
				bf.LaxHTMLBlocks|
				bf.Strikethrough|
				bf.SpaceHeadings|
				bf.HeadingIDs|
				bf.AutoHeadingIDs|
				bf.Titleblock|
				bf.BackslashLineBreak|
				bf.DefinitionLists|
				bf.NoEmptyLineBeforeBlock,
		),
	)

	html = colon.ReplaceAll(html, []byte(`:`))

	log.Tracef(nil, "rendered markdown to html:\n%s", string(html))

	return string(html)
}

// DropDocumentLeadingH1 will drop leading H1 headings to prevent
// duplication of or visual conflict with page titles.
// NOTE: This is intended only to operate on the whole markdown document.
// Operating on individual lines will clear them if the begin with `#`.
func DropDocumentLeadingH1(
	markdown []byte,
) []byte {
	h1 := regexp.MustCompile(`^#[^#].*\n`)
	markdown = h1.ReplaceAll(markdown, []byte(""))
	return markdown
}
