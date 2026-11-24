#!/usr/bin/env python3
"""Split each Go file of the old layout into the group files of the design.

Reads design.transcriptextraction-lexicographic-decomposition.txt section 5 to
learn the group of each file, then moves each top-level declaration into the
file of its group. Prints the plan and writes nothing unless --write is given.
"""

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MODULE = "github.com/kcg-ml/kcg-llm.transcript-extraction"

# The group of each declaration, by the name it declares. A name not listed
# stays in the group its file already maps to.
DECLARATION_GROUP = {
    # summarywindow: the prompt group holds the prompt and what it reads
    "namedReferenceKindSet": "prompt",
    "RenderPrompt": "prompt",
    "renderMessageBlock": "prompt",
    "referenceListOfMessage": "prompt",
    # phototable: the index group holds the index write
    "appendPhotoRowsToIndex": "index",
}

# The group of each old file, in the order the design lists them.
FILE_GROUP_LIST = {
    "internal/textrule/textrule.go": ["path", "pattern"],
    "internal/csvtable/csvtable.go": ["table"],
    "internal/telegramexport/telegramexport.go": ["read", "label", "write"],
    "internal/messagesplit/messagesplit.go": ["source", "paragraph", "sentence", "part"],
    "internal/referenceextraction/referenceextraction.go": ["kind", "sentence", "refpattern", "collect"],
    "internal/summarywindow/summarywindow.go": ["window", "prompt"],
    "internal/relevancewindow/relevancewindow.go": ["window", "relprompt", "vote"],
    "internal/relevancesolo/relevancesolo.go": ["solo"],
    "internal/relevanceconsensus/relevanceconsensus.go": ["consensus"],
    "internal/photoextraction/photoextraction.go": ["table", "photoprompt"],
    "internal/phototable/phototable.go": ["index"],
    "internal/summaryaggregation/summaryaggregation.go": ["parse", "rollup", "index"],
    "internal/durablejob/durablejob.go": ["unit", "store", "call", "answer", "progress", "run"],
    "internal/jobunit/jobunit.go": ["chainunit"],
    "internal/pipelinechain/pipelinechain.go": ["stage", "chainrun"],
    "cmd/transcriptextraction/main.go": ["subcommand", "command"],
    "cmd/csvcheck/main.go": ["csvcheck"],
}

# The project and the nNNNN block of each old file, from design section 5.
FILE_PROJECT = {
    "internal/textrule/textrule.go": ("t0-transcriptextraction-table", 100),
    "internal/csvtable/csvtable.go": ("t0-transcriptextraction-table", 120),
    "internal/telegramexport/telegramexport.go": ("t1-transcriptextraction-export", 200),
    "internal/messagesplit/messagesplit.go": ("t1-transcriptextraction-split", 300),
    "internal/referenceextraction/referenceextraction.go": ("t1-transcriptextraction-reference", 400),
    "internal/summarywindow/summarywindow.go": ("t2-transcriptextraction-summary", 600),
    "internal/relevancewindow/relevancewindow.go": ("t2-transcriptextraction-relevance", 700),
    "internal/relevancesolo/relevancesolo.go": ("t2-transcriptextraction-relevance", 730),
    "internal/relevanceconsensus/relevanceconsensus.go": ("t2-transcriptextraction-relevance", 740),
    "internal/photoextraction/photoextraction.go": ("t2-transcriptextraction-photo", 800),
    "internal/phototable/phototable.go": ("t2-transcriptextraction-photo", 820),
    "internal/summaryaggregation/summaryaggregation.go": ("t2-transcriptextraction-aggregate", 900),
    "internal/durablejob/durablejob.go": ("t3-transcriptextraction-job", 1000),
    "internal/jobunit/jobunit.go": ("t4-transcriptextraction-chain", 1100),
    "internal/pipelinechain/pipelinechain.go": ("t4-transcriptextraction-chain", 1110),
    "cmd/transcriptextraction/main.go": ("te-transcriptextraction-main", 1200),
    "cmd/csvcheck/main.go": ("te-transcriptextraction-main", 1300),
}

# The group of each declaration, by the file that holds it today and the name
# it declares. A name not listed takes the group of the file's first entry.
GROUP_OF_NAME = {
    "internal/textrule/textrule.go": {
        "ProjectFolderPath": "path",
        "ProjectFolderVariableName": "path",
        "DataFolderPath": "path",
        "PromptFolderPath": "path",
        "OutputFolderPath": "path",
        "LogFolderPath": "path",
        "UrlPattern": "pattern",
        "UrlTrailingCharacters": "pattern",
        "ArxivUrlPattern": "pattern",
        "ArxivBarePattern": "pattern",
        "DoiUrlPattern": "pattern",
        "DoiBarePattern": "pattern",
        "CleanUrl": "pattern",
        "UrlHostName": "pattern",
        "WordCount": "pattern",
        "MessageKeyOf": "pattern",
        "MonthOf": "pattern",
        "CutRuneText": "pattern",
        "BoolText": "pattern",
        "padMessageId": "pattern",
    },
    "internal/telegramexport/telegramexport.go": {
        "MessageTableColumnNames": "read",
        "MethodKeywordList": "read",
        "ReasonKeywordList": "read",
        "ExperimentKeywordList": "read",
        "ClaimKeywordList": "read",
        "ExportMessage": "read",
        "ExportEntity": "read",
        "MessageTextOf": "read",
        "UrlKindOf": "read",
        "SentenceAround": "read",
        "exportFile": "read",
        "referenceLabelList": "label",
        "ReferenceLabelPattern": "label",
        "labelBefore": "label",
        "Run": "write",
        "writeAll": "write",
        "containsText": "write",
        "boolText": "write",
        "isAddressHostName": "read",
        "keywordHitsOf": "write",
        "textHashOf": "write",
        "sideTables": "write",
        "newSideTables": "write",
    },
    "internal/messagesplit/messagesplit.go": {
        "SplitModeList": "part",
        "UnbrokenMaxWordCount": "part",
        "PartTableColumnNames": "part",
        "SourceTypeOf": "source",
        "ParagraphListOf": "paragraph",
        "SentenceListOf": "sentence",
        "SeparatorPattern": "paragraph",
        "SentenceEndPattern": "sentence",
        "CodeFencePattern": "paragraph",
        "UrlCountPattern": "source",
        "ArxivCountPattern": "source",
        "IndentedCodePattern": "source",
        "wordChunkListOf": "part",
        "PartTextListOf": "part",
        "Run": "part",
        "boolText": "part",
    },
    "internal/referenceextraction/referenceextraction.go": {
        "ReferenceKindList": "kind",
        "ReferenceTableColumnNames": "kind",
        "MethodTermList": "kind",
        "DatasetTermList": "kind",
        "ModelTermList": "kind",
        "TitleLinePattern": "kind",
        "AuthorLinePattern": "kind",
        "SentenceSpan": "sentence",
        "SentenceSpanListOf": "sentence",
        "isSentenceEndCharacter": "sentence",
        "SentenceAround": "sentence",
        "UrlKindOf": "sentence",
        "WordBoundaryLeft": "refpattern",
        "WordBoundaryRight": "refpattern",
        "WordBoundaryLeft": "refpattern",
        "WordBoundaryRight": "refpattern",
        "TalkPattern": "refpattern",
        "ResultPattern": "refpattern",
        "ExperimentPattern": "refpattern",
        "ReasonPattern": "refpattern",
        "CausalPattern": "refpattern",
        "SuggestionPattern": "refpattern",
        "ClaimPattern": "refpattern",
        "TermPatternOf": "refpattern",
        "talkSpanListOf": "refpattern",
        "referenceCollector": "collect",
        "add": "collect",
        "cutText": "collect",
        "boolText": "collect",
        "Run": "collect",
        "sortedKeys": "collect",
    },
    "internal/summarywindow/summarywindow.go": {
        "PromptCharBudget": "prompt",
        "WindowTableColumnNames": "window",
        "SummaryFieldList": "prompt",
        "SummaryAnswerFormat": "prompt",
        "Window": "window",
        "windowListOfSize": "window",
        "monthWindowList": "window",
        "MessageWordCountOf": "window",
        "MonthOfMessageRow": "window",
        "namedReferenceKindSet": "prompt",
        "renderMessageBlock": "prompt",
        "referenceListOfMessage": "prompt",
        "RenderPrompt": "prompt",
        "Run": "prompt",
    },
    "internal/relevancewindow/relevancewindow.go": {
        "WindowSize": "window",
        "VoteOffsetList": "window",
        "MaxWordCount": "window",
        "RelevanceWindowTableColumnNames": "window",
        "RelevanceWindow": "window",
        "BuildWindowList": "window",
        "sliceWithin": "window",
        "PromptCharBudget": "relprompt",
        "RelevancePromptText": "relprompt",
        "RenderPrompt": "relprompt",
        "Run": "relprompt",
        "cutText": "relprompt",
        "RelevantReferenceKindSet": "vote",
        "RelevantLinkHostList": "vote",
        "TopicTermList": "vote",
        "TopicTermPattern": "vote",
        "buildTopicAlternation": "vote",
        "VoteResult": "vote",
        "VoteOnReference": "vote",
        "VoteOnLink": "vote",
        "VoteOnTopic": "vote",
    },
    "internal/relevanceconsensus/relevanceconsensus.go": {
        "VoteTableColumnNames": "consensus",
        "ConsensusTableColumnNames": "consensus",
        "VoteLinePattern": "consensus",
        "PromptLabelPattern": "consensus",
        "PromptKeyPattern": "consensus",
        "PromptKeyMapOf": "consensus",
        "ParseVoteMap": "consensus",
        "VoteRecord": "consensus",
        "VerdictOf": "consensus",
        "voteOfMessage": "consensus",
        "answerFilePathOf": "consensus",
        "cutText": "consensus",
        "Run": "consensus",
    },
    "internal/photoextraction/photoextraction.go": {
        "PhotoTableColumnNames": "table",
        "photoExportMessage": "table",
        "photoExportFile": "table",
        "MessageStartPattern": "table",
        "LoadExport": "table",
        "Run": "table",
        "cutText": "table",
        "PhotoPromptTemplate": "photoprompt",
    },
    "internal/phototable/phototable.go": {
        "PhotoAnswerFieldList": "index",
        "PhotoIndexTableColumnNames": "index",
        "AnswerFieldLinePattern": "index",
        "ParsePhotoAnswer": "index",
        "photoAnswerFilePathList": "index",
        "appendPhotoRowsToIndex": "index",
        "cutText": "index",
        "Run": "index",
    },
    "internal/summaryaggregation/summaryaggregation.go": {
        "ItemSeparatorText": "parse",
        "AnswerFieldLinePattern": "parse",
        "SummaryTableColumnNames": "parse",
        "SummaryItemTableColumnNames": "parse",
        "IndexTableColumnNames": "index",
        "IndexMessageTableColumnNames": "index",
        "IndexTermTableColumnNames": "index",
        "ParseAnswer": "parse",
        "SplitItemList": "parse",
        "ItemHeadOf": "parse",
        "summaryRecord": "parse",
        "summaryItemRecord": "parse",
        "loadSummaryRecordList": "parse",
        "readAnswerText": "parse",
        "jobNameOfWindowId": "parse",
        "summaryRowOf": "parse",
        "writeTableFile": "parse",
        "cutText": "parse",
        "boolText": "parse",
        "rollupGroupOfWindowId": "rollup",
        "rollupInstructionLineList": "rollup",
        "buildRollupPromptList": "rollup",
        "buildIndexTableList": "index",
        "termEntry": "index",
        "sortedSetKeys": "index",
        "Run": "index",
    },
    "internal/durablejob/durablejob.go": {
        "JobUnit": "unit",
        "JobSettings": "unit",
        "JobStore": "store",
        "NewJobStore": "store",
        "DoneFilePath": "store",
        "FailedFilePath": "store",
        "IsDone": "store",
        "IsFailed": "store",
        "WriteDone": "store",
        "WriteFailed": "store",
        "appendStateLine": "store",
        "ClearFailed": "store",
        "Counts": "store",
        "PiCallResult": "call",
        "CallPi": "call",
        "ShrinkImage": "call",
        "ErrorPrefixList": "answer",
        "AnswerErrorOf": "answer",
        "PositionLabelPattern": "answer",
        "MessageKeyPattern": "answer",
        "PromptPositionLabelPattern": "answer",
        "PromptMessageKeyPattern": "answer",
        "answerLabelListOf": "answer",
        "promptLabelListOf": "answer",
        "AnswerWrongnessOf": "answer",
        "RunCounters": "progress",
        "addOk": "progress",
        "addFail": "progress",
        "addInFlight": "progress",
        "snapshot": "progress",
        "formatEtaText": "progress",
        "progressLineOf": "progress",
        "roundToTenth": "progress",
        "cutText": "run",
        "runUnit": "run",
        "Run": "run",
    },
    "internal/jobunit/jobunit.go": {
        "JobNameList": "chainunit",
        "IsKnownJobName": "chainunit",
        "unitListOfWindowKind": "chainunit",
        "unitListOfRelevance": "chainunit",
        "unitListOfPhoto": "chainunit",
        "UnitListOf": "chainunit",
    },
    "internal/pipelinechain/pipelinechain.go": {
        "TextExportFolderName": "stage",
        "ImageExportFolderName": "stage",
        "StageNameList": "stage",
        "ChainSettings": "stage",
        "runRuleStage": "stage",
        "runJobStage": "stage",
        "RunRules": "chainrun",
        "Run": "chainrun",
    },
    "internal/relevancesolo/relevancesolo.go": {"Run": "solo", "VoteCount": "solo"},
    "internal/csvtable/csvtable.go": {
        "Table": "table",
        "NewTable": "table",
        "AppendRow": "table",
        "RowCount": "table",
        "ReadTable": "table",
        "ReadTableIfPresent": "table",
        "WriteTable": "table",
        "AppendRowsToTable": "table",
        "RelativePath": "table",
    },
    "cmd/transcriptextraction/main.go": {
        "DefaultModelName": "subcommand",
        "DefaultProviderName": "subcommand",
        "subcommandHelpText": "subcommand",
        "runExtract": "subcommand",
        "runSplit": "subcommand",
        "runReferences": "subcommand",
        "runWindows": "subcommand",
        "runRelevanceWindows": "subcommand",
        "runRelevanceSolo": "subcommand",
        "runRelevanceConsensus": "subcommand",
        "runPhotos": "subcommand",
        "runPhotoIndex": "subcommand",
        "runAggregate": "subcommand",
        "addJobFlags": "subcommand",
        "runJob": "subcommand",
        "runPipeline": "subcommand",
        "main": "command",
    },
    "cmd/csvcheck/main.go": {"checkTable": "csvcheck", "main": "csvcheck"},
}

# The package name of each group, and the project that holds it.
GROUP_PACKAGE = {}


def project_package(project):
    """The Go package name of a project folder: the part after the tier."""
    return project.split("-", 1)[1].replace("-", "")


def split_declarations(source):
    """Return (header, [(name, text)]) where header holds the package clause."""
    lines = source.splitlines(keepends=True)
    header_end = 0
    for index, line in enumerate(lines):
        if line.startswith("package "):
            header_end = index + 1
            break
    header = "".join(lines[:header_end])
    body = lines[header_end:]
    # The import block belongs to the header, not to the first declaration.
    while body and body[0].strip() == "":
        body.pop(0)
    if body and body[0].startswith("import ("):
        while body and body[0].strip() != ")":
            header += body.pop(0)
        header += body.pop(0)
        header += "\n"
    elif body and body[0].startswith("import "):
        header += body.pop(0) + "\n"
    declarations = []
    current = None
    # A comment block that sits above a declaration documents it, so it is
    # held here and written in front of the declaration it belongs to. The
    # block starts after a blank line, so a comment that follows a blank line
    # is never the tail of the declaration above it.
    comment_buffer = []
    after_blank = True
    for line in body:
        starts = re.match(r"^(func|type|var|const) ", line)
        if starts:
            if current is not None:
                declarations.append(current)
            current = comment_buffer + [line]
            comment_buffer = []
            after_blank = False
            continue
        if line.strip() == "":
            after_blank = True
            if current is not None:
                current.append(line)
            continue
        if line.lstrip().startswith("//") and (after_blank or comment_buffer):
            # A comment block belongs to the declaration below it, so every
            # line of the block is held until that declaration starts.
            comment_buffer.append(line)
            after_blank = False
            continue
        if current is None:
            comment_buffer.append(line)
            after_blank = False
            continue
        current.append(line)
        after_blank = False
    if current is not None:
        declarations.append(current)
    result = []
    for chunk in declarations:
        text = "".join(chunk)
        name = declaration_name(text)
        if name == "__unknown__":
            continue
        result.append((name, text))
    return header, result


def declaration_name(text):
    # A doc comment sits above its declaration, so the first line that is not
    # a comment names the declaration.
    first = ""
    for line in text.splitlines():
        if line.lstrip().startswith("//") or line.strip() == "":
            continue
        first = line
        break
    match = re.match(r"^func \(([a-zA-Z]+) \*?([A-Za-z]+)\) ([A-Za-z0-9_]+)", first)
    if match:
        return match.group(3)
    match = re.match(r"^func ([A-Za-z0-9_]+)", first)
    if match:
        return match.group(1)
    match = re.match(r"^(?:type|var|const) ([A-Za-z0-9_]+)", first)
    if match:
        return match.group(1)
    match = re.match(r"^const \(", first)
    if match:
        return "WordBoundaryLeft"
    return "__unknown__"


def main():
    write = "--write" in sys.argv
    plan = {}
    for old_file, group_list in FILE_GROUP_LIST.items():
        path = os.path.join(ROOT, old_file)
        source = open(path, encoding="utf-8").read()
        header, declarations = split_declarations(source)
        header_imports = ""
        project, base_number = FILE_PROJECT[old_file]
        group_of_name = GROUP_OF_NAME[old_file]
        for name, text in declarations:
            group = group_of_name.get(name)
            if group is None:
                print(f"UNMAPPED {old_file} {name}")
                continue
            number = base_number + group_list.index(group) * 10
            key = (project, group, number)
            plan.setdefault(key, []).append((old_file, name, text))
    for key in sorted(plan):
        project, group, number = key
        print(f"{number} {project}.{group}: {len(plan[key])} declarations "
              f"{[name for _, name, _ in plan[key]]}")
    if not write:
        return
    for key in sorted(plan):
        project, group, number = key
        folder = os.path.join(ROOT, "internal", project, group)
        if project.startswith("te-"):
            folder = os.path.join(ROOT, "cmd", group)
        os.makedirs(folder, exist_ok=True)
        file_name = f"n{number:04d}.{project}.[{group}].go"
        # A te- group holds a main function, so its package is main. The
        # subcommand group is a library the command group imports.
        package_name = group
        if project.startswith("te-") and group != "subcommand":
            package_name = "main"
        # One file holds one type. A group whose declarations name 2 or more
        # types is written as one file per type, and each function follows the
        # type its receiver or its first parameter names.
        def is_type_declaration(declaration_text):
            for line in declaration_text.splitlines():
                stripped = line.strip()
                if not stripped or stripped.startswith("//"):
                    continue
                return stripped.startswith("type ")
            return False

        type_name_list = [
            name for _, name, text in plan[key]
            if is_type_declaration(text)
        ]
        if len(type_name_list) <= 1:
            file_group_list = [(file_name, plan[key])]
        else:
            file_group_list = []
            for type_index, type_name in enumerate(type_name_list):
                number_here = number + type_index
                file_group_list.append((
                    f"n{number_here:04d}.{project}.[{group}].go",
                    [declaration for declaration in plan[key]
                     if declaration[1] == type_name],
                ))
            # A function that names no type of this group goes to the last
            # file, so no declaration is lost.
            leftover_list = [
                declaration for declaration in plan[key]
                if declaration[1] not in type_name_list
            ]
            if leftover_list:
                last_name, last_list = file_group_list[-1]
                file_group_list[-1] = (last_name, last_list + leftover_list)
        for file_name_here, declaration_list in file_group_list:
            body = []
            for _, name, text in declaration_list:
                body.append(text.rstrip("\n"))
            content = f"package {package_name}\n\n" + "\n\n".join(body) + "\n"
            if header_imports:
                content = content.replace(f"package {package_name}\n",
                                          f"package {package_name}\n\n{header_imports}\n", 1)
            open(os.path.join(folder, file_name_here), "w", encoding="utf-8").write(content)
            print(f"wrote {folder}/{file_name_here}")


if __name__ == "__main__":
    main()