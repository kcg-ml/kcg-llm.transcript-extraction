#!/usr/bin/env python3
"""Give each group file the imports its own body uses, and the qualified names
of the declarations it now reaches across a package boundary."""

import os
import re
import subprocess
import sys

MODULE = "github.com/kcg-ml/kcg-llm.transcript-extraction"

STDLIB = {
    "bytes": "bytes", "context": "context", "crypto/sha1": "sha1",
    "encoding/csv": "csv", "encoding/hex": "hex", "encoding/json": "json",
    "errors": "errors", "flag": "flag", "fmt": "fmt", "io": "io",
    "os": "os", "os/exec": "exec", "path/filepath": "filepath",
    "regexp": "regexp", "sort": "sort", "strconv": "strconv",
    "strings": "strings", "sync": "sync", "time": "time",
    "unicode": "unicode", "unicode/utf8": "utf8",
}

# The package that holds each declaration the old single-package files shared.
# A name listed here is qualified with the package name when a file that does
# not hold it uses it.
DECLARATION_PACKAGE = {}

# Every package name the split wrote, so a qualifier can be checked against it.
PACKAGE_NAME_SET = set()

# The package name of each old single file, so a qualifier that names an old
# package is read as the group the split gave the name.
OLD_PACKAGE_FOLDER = {
    "textrule": "pattern", "csvtable": "table", "telegramexport": "write",
    "messagesplit": "part", "referenceextraction": "collect",
    "summarywindow": "window", "relevancewindow": "prompt",
    "relevancesolo": "solo", "relevanceconsensus": "consensus",
    "photoextraction": "table", "phototable": "index",
    "summaryaggregation": "index", "durablejob": "run", "jobunit": "unit",
    "pipelinechain": "run",
}


# The group folder the split gave each declaration of an old single file, so a
# qualifier that names an old package is resolved to exactly one folder even
# when 2 projects hold a group of the same name (index, table, prompt, run).
OLD_PACKAGE_GROUP_FOLDER = {}

# The group folder of each declaration of an old single file that the split
# gave 2 or more groups, keyed by "<old package name>.<declaration name>", so
# summaryaggregation.IndexTableColumnNames resolves to aggregate/index and
# summaryaggregation.ParseSummaryAnswer resolves to aggregate/parse.
OLD_PACKAGE_DECLARATION_FOLDER = {}


def folder_of_qualifier(qualifier, holder_list, own_package):
    """Return the one folder a qualifier names, or None when it names none.

    A qualifier is one of 3 things: the name of an old single package
    (summaryaggregation), the package name of a group (consensus), or the name
    of a group folder (index). The old package name is checked first, because
    a group folder name is shared by 2 projects and cannot pick a folder.
    """
    if qualifier in OLD_PACKAGE_GROUP_FOLDER:
        wanted_folder = OLD_PACKAGE_GROUP_FOLDER[qualifier]
        for _, folder in holder_list:
            if folder == wanted_folder:
                return folder
        # The qualifier names an old package that does not declare this name,
        # so it is not the qualifier of this use.
        return None
    for package_name, folder in holder_list:
        if package_name == qualifier:
            return folder
    for _, folder in holder_list:
        if os.path.basename(folder) == qualifier:
            return folder
    return None


def group_files():
    result = []
    for folder, _, names in os.walk("."):
        if ".build" in folder or "/extract." in folder:
            continue
        for name in names:
            if name.endswith(".go"):
                result.append(os.path.join(folder, name))
    return sorted(result)


def declarations_of(path):
    """Return the top-level names a file declares."""
    text = open(path, encoding="utf-8").read()
    names = set()
    for match in re.finditer(r"^func \(([a-zA-Z]+) \*?([A-Za-z0-9]+)\) ([A-Za-z0-9_]+)", text, re.M):
        names.add(match.group(3))
    for match in re.finditer(r"^func ([A-Za-z0-9_]+)", text, re.M):
        names.add(match.group(1))
    for match in re.finditer(r"^(?:type|var|const) ([A-Za-z0-9_]+)", text, re.M):
        names.add(match.group(1))
    for match in re.finditer(r"^\t([A-Za-z0-9_]+)\s+(?:struct|interface)\s*\{", text, re.M):
        names.add(match.group(1))
    for block in re.finditer(r"^const \(\n(.*?)^\)", text, re.M | re.S):
        for match in re.finditer(r"^\t([A-Za-z0-9_]+)\s*=", block.group(1), re.M):
            names.add(match.group(1))
    return names


def build_old_package_group_folder():
    """Fill OLD_PACKAGE_GROUP_FOLDER from the split plan.

    The splitter knows which group folder each declaration of an old single
    file went to, so the map is read from it rather than guessed from a
    folder basename that 2 projects share. An old file that the split gave 2
    or more groups has no single folder, so its bare name maps to no folder
    and a qualifier of that name is resolved per declaration instead.
    """
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    import split_into_groups as split_plan
    for old_file, group_list in split_plan.FILE_GROUP_LIST.items():
        project, _ = split_plan.FILE_PROJECT[old_file]
        old_package_name = os.path.basename(old_file).replace(".go", "")
        folder_of_group = {}
        for group in group_list:
            if project.startswith("te-"):
                folder_of_group[group] = os.path.join("cmd", group)
            else:
                folder_of_group[group] = os.path.join("internal", project, group)
        if len(group_list) == 1:
            OLD_PACKAGE_GROUP_FOLDER[old_package_name] = folder_of_group[group_list[0]]
        for group, folder in folder_of_group.items():
            OLD_PACKAGE_GROUP_FOLDER[old_package_name + "." + group] = folder
        group_of_name = split_plan.GROUP_OF_NAME[old_file]
        for declaration_name, group in group_of_name.items():
            OLD_PACKAGE_DECLARATION_FOLDER[
                old_package_name + "." + declaration_name] = folder_of_group[group]


# A name a group uses across a package boundary must be exported, so the
# splitter's lowercase spelling is replaced by the exported one.
CROSS_GROUP_EXPORTED_NAME = {
    "boolText": "BoolText",
    "labelBefore": "LabelBefore",
    "talkSpanListOf": "TalkSpanListOf",
    "windowListOfSize": "WindowListOfSize",
    "monthWindowList": "MonthWindowList",
    "exportFile": "ExportFile",
    "progressLineOf": "ProgressLineOf",
    "roundToTenth": "RoundToTenth",
    "summaryRecord": "SummaryRecord",
    "addOk": "AddOk",
    "addFail": "AddFail",
    "addInFlight": "AddInFlight",
    "buildRollupPromptList": "BuildRollupPromptList",
    "summaryItemRecord": "SummaryItemRecord",
    "loadSummaryRecordList": "LoadSummaryRecordList",
    "readAnswerText": "ReadAnswerText",
    "summaryRowOf": "SummaryRowOf",
    "writeTableFile": "WriteTableFile",
    "runRuleStage": "RunRuleStage",
    "runJobStage": "RunJobStage",
    "subcommandHelpText": "SubcommandHelpText",
    "runExtract": "RunExtract",
    "runSplit": "RunSplit",
    "runReferences": "RunReferences",
    "runWindows": "RunWindows",
    "runRelevanceWindows": "RunRelevanceWindows",
    "runRelevanceSolo": "RunRelevanceSolo",
    "runRelevanceConsensus": "RunRelevanceConsensus",
    "runPhotos": "RunPhotos",
    "runPhotoIndex": "RunPhotoIndex",
    "runAggregate": "RunAggregate",
    "runJob": "RunJob",
    "runPipeline": "RunPipeline",
    "addJobFlags": "AddJobFlags",
}

OLD_PACKAGE_SINGLE_FOLDER = {}
OLD_PACKAGE_NAME_SET = set()


STRING_LITERAL_PATTERN = re.compile(r'`[^`]*`|"(?:[^"\\\n]|\\.)*"')


def rewrite_outside_strings(text, pattern, replacement):
    """Rewrite a pattern in the code of a file, never inside a string literal.

    A prompt is built from string literals, so a rewrite that reached one
    would change the text the model reads.
    """
    piece_list = []
    last_end = 0
    for match in STRING_LITERAL_PATTERN.finditer(text):
        piece_list.append(re.sub(pattern, replacement, text[last_end:match.start()]))
        piece_list.append(match.group(0))
        last_end = match.end()
    piece_list.append(re.sub(pattern, replacement, text[last_end:]))
    return "".join(piece_list)


def main():
    DECLARATION_PACKAGE.clear()
    PACKAGE_NAME_SET.clear()
    OLD_PACKAGE_GROUP_FOLDER.clear()
    OLD_PACKAGE_DECLARATION_FOLDER.clear()
    build_old_package_group_folder()
    OLD_PACKAGE_SINGLE_FOLDER.clear()
    OLD_PACKAGE_SINGLE_FOLDER.update(
        {name: os.path.basename(folder)
         for name, folder in OLD_PACKAGE_GROUP_FOLDER.items()
         if "." not in name}
    )
    OLD_PACKAGE_NAME_SET.clear()
    OLD_PACKAGE_NAME_SET.update(OLD_PACKAGE_SINGLE_FOLDER)
    OLD_PACKAGE_NAME_SET.update(
        name.split(".")[0] for name in OLD_PACKAGE_DECLARATION_FOLDER
    )
    files = group_files()
    package_of_file = {}
    for path in files:
        text = open(path, encoding="utf-8").read()
        match = re.search(r"^package (\w+)", text, re.M)
        package_of_file[path] = match.group(1)
        PACKAGE_NAME_SET.add(match.group(1))
    # The package of each declaration, and the folder that holds it. A name
    # that 2 packages declare (Run, for one) is reached by the package the old
    # single file imported, so every holder is kept and the scan reads the
    # qualified form the splitter wrote.
    for path in files:
        for name in declarations_of(path):
            # A declaration the splitter left lowercase is exported on disk
            # by the same rename the body gets, so the map holds the exported
            # spelling.
            exported_name = CROSS_GROUP_EXPORTED_NAME.get(name, name)
            DECLARATION_PACKAGE.setdefault(exported_name, []).append(
                (package_of_file[path], os.path.dirname(path))
            )

    # BoolText is written into the table pattern group by this script, so it
    # is registered here rather than read from disk.
    DECLARATION_PACKAGE.setdefault("BoolText", []).append(
        ("pattern", os.path.join("internal", "t0-transcriptextraction-table", "pattern"))
    )

    for path in files:
        text = open(path, encoding="utf-8").read()
        own_package = package_of_file[path]
        own_names = declarations_of(path)
        body = re.sub(r"^package \w+\n", "", text)
        body = re.sub(r"^import \(.*?^\)\n", "", body, flags=re.M | re.S)
        body = re.sub(r'^import ".*?"\n', "", body, flags=re.M)
        # The scan reads a canonical form of the body: every space around a
        # dot or a bracket is removed, so a name is always followed by the
        # same character whatever the splitter wrote. The file keeps its own
        # text.
        # A comment names things the code does not use, so it is removed
        # before the scan. The written file keeps its comments.
        code_only = re.sub(r"/\*.*?\*/", "", body, flags=re.S)
        code_only = re.sub(r"(?m)^\s*//.*$", "", code_only)
        code_only = re.sub(r"\s//[^\n]*", "", code_only)
        # A string literal holds text, not a name, so it is emptied too. A
        # regex such as `do|use|add` would otherwise read as a use of add.
        code_only = re.sub(r'`[^`]*`', '""', code_only, flags=re.S)
        code_only = re.sub(r'"(?:[^"\\\n]|\\.)*"', '""', code_only)
        scan_text = re.sub(r"\s*([.(),*\[\]{}])\s*", lambda m: m.group(1), code_only)
        # A qualified name is a use of the package, and the canonical form
        # glues the dot to both names, so the dot is split first.
        scan_text = re.sub(r"([A-Za-z0-9_]+)\.([A-Za-z0-9_]+)", lambda m: m.group(1) + " . " + m.group(2), scan_text)
        # A call is a use of the name, and the canonical form glues the name
        # to its bracket.
        scan_text = re.sub(r"([A-Za-z0-9_]+)\(", lambda m: m.group(1) + " (", scan_text)
        # A parameter, a result, and a struct field type are uses of the type,
        # and they sit against a bracket or after a field name.
        scan_text = re.sub(r"([(,])(\*?)([A-Z][A-Za-z0-9_]*)", lambda m: m.group(1) + " " + m.group(2) + m.group(3), scan_text)
        scan_text = re.sub(r"(\*?)([A-Z][A-Za-z0-9_]*)([),])", lambda m: " " + m.group(1) + m.group(2) + m.group(3), scan_text)
        scan_text = re.sub(r"\b([A-Za-z0-9_]+) ([A-Z][A-Za-z0-9_]*)", lambda m: m.group(1) + " " + m.group(2), scan_text)
        # A bare name that follows a comma is a use of the name.
        scan_text = re.sub(r",([A-Za-z0-9_]+)", lambda m: ", " + m.group(1), scan_text)
        # A type name that starts lowercase (an unexported struct) is a use of
        # the type too, and it follows a parameter name.
        scan_text = re.sub(r"([(,])(\*?)([a-z][A-Za-z0-9_]*)", lambda m: m.group(1) + " " + m.group(2) + m.group(3), scan_text)
        # A name that a file of the same Go package declares stays
        # unqualified. A group folder is the package, so its siblings are the
        # files beside it; the 2 commands are 2 entry points, so each of them
        # is a package of its own.
        own_group_folder = os.path.dirname(path)
        # The 7 copies of cutText and the 4 copies of boolText collapse to the
        # one in the pattern package, so a copy outside it is deleted. The
        # deletion walks lines, because a brace count inside a regex is
        # fragile.
        table_pattern_folder = os.path.join(
            "internal", "t0-transcriptextraction-table", "pattern"
        )
        if os.path.abspath(own_group_folder) == os.path.abspath(table_pattern_folder):
            # The 4 copies of boolText collapse into this package, so the one
            # copy is written here.
            if "func BoolText(" not in body:
                body = body.rstrip("\n") + """

// BoolText writes 1 for true and 0 for false, the shape the tables use.

func BoolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
"""
        if os.path.abspath(own_group_folder) != os.path.abspath(table_pattern_folder):
            kept_line_list = []
            line_list = body.splitlines(keepends=True)
            index = 0
            while index < len(line_list):
                line = line_list[index]
                if re.match(r"^func (cutText|boolText)\(", line):
                    # Drop the doc comment above the declaration.
                    while kept_line_list and kept_line_list[-1].lstrip().startswith("//"):
                        kept_line_list.pop()
                    if kept_line_list and kept_line_list[-1].strip() == "":
                        kept_line_list.pop()
                    # Drop the body up to the closing brace at column 0.
                    while index < len(line_list) and line_list[index].rstrip("\n") != "}":
                        index += 1
                    index += 1
                    # Drop the blank line that followed the declaration.
                    if index < len(line_list) and line_list[index].strip() == "":
                        index += 1
                    continue
                kept_line_list.append(line)
                index += 1
            body = "".join(kept_line_list)
        for old_name, exported_name in CROSS_GROUP_EXPORTED_NAME.items():
            body = re.sub(r"(?<![\w.])" + old_name + r"\b", exported_name, body)
        own_project_folder = os.path.dirname(own_group_folder)
        own_package_names = set(own_names) | {"main"}
        used_std = set()
        for import_path, package_name in STDLIB.items():
            if re.search(r"(?<![\w])" + package_name + r"\s*\.", scan_text):
                used_std.add(import_path)
        used_internal = {}
        own_project = os.path.dirname(os.path.dirname(path))
        # A name that 2 packages declare (Run, for one) is reached by the
        # package the old single file imported, so the scan reads the
        # qualified form the splitter wrote, not the bare name.
        for name, holder_list in DECLARATION_PACKAGE.items():
            if name in own_package_names:
                continue
            if name == "main":
                # Each of the 2 commands is its own package with its own main.
                continue
            qualified_use = r"(?<![\w.])" + r"([A-Za-z0-9_]+)\s*\.\s*" + name + r"\b"
            bare_use = r"(?<![\w.])" + name + r"\b"
            qualified_match = re.search(qualified_use, scan_text)
            if qualified_match and (
                    qualified_match.group(1) + "." + name) in OLD_PACKAGE_DECLARATION_FOLDER:
                # The qualifier names an old single package that the split
                # gave 2 or more groups, so the folder is the one that holds
                # this declaration.
                wanted_folder = OLD_PACKAGE_DECLARATION_FOLDER[
                    qualified_match.group(1) + "." + name]
                if wanted_folder == own_group_folder:
                    continue
                used_internal[os.path.basename(wanted_folder)] = wanted_folder
                continue
            if qualified_match and qualified_match.group(1) in OLD_PACKAGE_GROUP_FOLDER:
                # The qualifier names an old single package, so the folder is
                # the one the split gave that package, never a folder that
                # another project happens to name the same way.
                wanted_folder = OLD_PACKAGE_GROUP_FOLDER[qualified_match.group(1)]
                if any(folder == wanted_folder for _, folder in holder_list):
                    if wanted_folder == own_group_folder:
                        continue
                    used_internal[os.path.basename(wanted_folder)] = wanted_folder
                    continue
                qualified_match = None
            if qualified_match:
                qualifier = qualified_match.group(1)
                target_folder = folder_of_qualifier(qualifier, holder_list, own_package)
                if target_folder is None:
                    # The qualifier names no package, so the use is bare.
                    qualified_match = None
                else:
                    used_internal[os.path.basename(target_folder)] = target_folder
                    continue
            if not re.search(bare_use, scan_text):
                continue
            if len(holder_list) > 1:
                # A name that 2 packages declare (Run, for one) is reached by
                # the package the old single file imported, so the scan reads
                # the qualified form the splitter wrote, not the bare name.
                continue
            for package_name, folder in holder_list:
                if folder == own_group_folder:
                    continue
                used_internal[package_name] = folder
        # A name this file uses bare but another package declares must be
        # written with that package in front, or Go reads it as undefined.
        for package_name, folder in used_internal.items():
            for other in files:
                if os.path.abspath(os.path.dirname(other)) != os.path.abspath(folder):
                    continue
                declared_name_set = set(declarations_of(other))
                if os.path.abspath(os.path.dirname(other)) == os.path.abspath(
                        os.path.join("internal", "t0-transcriptextraction-table", "pattern")):
                    # BoolText is written by this script, so it is not on disk
                    # when the declarations are read.
                    declared_name_set.add("BoolText")
                for declared_name in declared_name_set:
                    if declared_name in own_package_names or declared_name == "main":
                        continue
                    # An unexported name is not reachable from another
                    # package, so only an exported one is qualified.
                    if not declared_name[:1].isupper():
                        continue
                    # A declaration line names the declaration, so it is not
                    # a use: `func Run(` must stay as it is.
                    body = rewrite_outside_strings(
                        body,
                        r"(?<![\w.])(?!(?:func|type|var|const) )" + declared_name + r"\b",
                        package_name + "." + declared_name,
                    )
                    # A struct field line declares a field, so it is not a
                    # use of the package name that spells the same word.
                    body = re.sub(
                        r"(?m)^(\t+)" + package_name + r"\.([A-Za-z_][A-Za-z0-9_]*)(\s+)(\S)",
                        r"\1\2\3\4",
                        body,
                    )
                    # A struct literal key names a field, so it is not a use
                    # either: `path.ProjectFolderPath:` reads
                    # `ProjectFolderPath:`.
                    body = re.sub(
                        r"(?m)^(\t+)" + package_name + r"\.([A-Za-z_][A-Za-z0-9_]*):",
                        r"\1\2:",
                        body,
                    )
        # A qualifier that names an old single package becomes the package
        # name the split gave it, so `textrule.CutRuneText` reads
        # `pattern.CutRuneText`.
        def old_qualifier_replacement(match):
            old_name, declared_name = match.group(1), match.group(2)
            if old_name not in OLD_PACKAGE_NAME_SET:
                return match.group(0)
            key = old_name + "." + declared_name
            if key in OLD_PACKAGE_DECLARATION_FOLDER:
                return os.path.basename(OLD_PACKAGE_DECLARATION_FOLDER[key]) + "." + declared_name
            if old_name in OLD_PACKAGE_SINGLE_FOLDER:
                return OLD_PACKAGE_SINGLE_FOLDER[old_name] + "." + declared_name
            return match.group(0)

        body = re.sub(
            r"(?<![\w.])([a-z][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)",
            old_qualifier_replacement,
            body,
        )
        # The 7 copies of cutText collapse to the one in the pattern package.
        # A declaration line is not a use, so it is left for the caller to
        # delete.
        body = re.sub(
            r"(?<![\w.])(?!(?:func|type|var|const) )cutText\(",
            "pattern.CutRuneText(",
            body,
        )
        # A package the body now names is imported from the folder that
        # declares the name, so `pattern.TermPatternOf` reads the reference
        # pattern group and `pattern.CutRuneText` reads the table one.
        # The cutText collapse names the table pattern package, so that
        # folder is imported even when the file also reads the reference
        # pattern group.
        if re.search(r"(?<![\w.])pattern\.CutRuneText\(", body):
            used_internal["pattern"] = os.path.join(
                "internal", "t0-transcriptextraction-table", "pattern"
            )
        # A qualifier resolves to the folder that declares the name it
        # carries, so `table.NewTable` reads the table group and
        # `table.PhotoTableColumnNames` reads the photo table group.
        for qualifier, declared_name in set(
            re.findall(r"(?<![\w.])([a-z][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)", body)
        ):
            if qualifier in own_package_names:
                continue
            holder_list = DECLARATION_PACKAGE.get(declared_name, [])
            for package_name, folder in holder_list:
                if folder == own_group_folder:
                    continue
                if package_name != qualifier:
                    continue
                used_internal[qualifier] = folder
                break
        # A parameter named `table` shadows the table package, so the
        # parameter is renamed and its method calls follow it.
        if "table *table.Table" in body:
            body = body.replace("table *table.Table", "tableObject *table.Table")
            for method_name in ("WriteTable", "RowCount"):
                body = body.replace("table." + method_name + "(",
                                    "tableObject." + method_name + "(")
        # A declaration line names the declaration, so no qualifier belongs on
        # it: `func table.WriteTable(` is written `func WriteTable(`.
        body = re.sub(r"(?m)^func [a-z][A-Za-z0-9_]*\.([A-Za-z_][A-Za-z0-9_]*)\(",
                      r"func \1(", body)
        # A declaration line names the declaration, so the export rename must
        # not reach it: `func writeTable(` stays a function, not a method.
        for old_name, new_name in CROSS_GROUP_EXPORTED_NAME.items():
            body = re.sub(r"(?m)^func " + old_name + r"\(",
                          "func " + new_name + "(", body)
            body = re.sub(r"(?m)^type " + old_name + r" ",
                          "type " + new_name + " ", body)
            # A qualified call site names the package that declares the
            # name, so `table.WriteTable` reads `parse.WriteTable`.
            # A qualified call site names the package that declares the name,
            # so `table.WriteTable` reads `parse.WriteTable` when the file is
            # not in the table package. A method call such as
            # `counters.addFail(` keeps its receiver.
            holder_package_name = ""
            for holder_package, holder_folder in DECLARATION_PACKAGE.get(new_name, []):
                if os.path.abspath(holder_folder) == os.path.abspath(own_group_folder):
                    continue
                holder_package_name = holder_package
                break

            def qualified_name_replacement(match, holder=holder_package_name):
                qualifier = match.group(1)
                if qualifier not in PACKAGE_NAME_SET:
                    return qualifier + "." + new_name
                if holder and qualifier != holder:
                    return holder + "." + new_name
                return qualifier + "." + new_name

            body = re.sub(
                r"(?<![\w.])([a-z][A-Za-z0-9_]*)\." + old_name + r"\b",
                qualified_name_replacement,
                body,
            )

        # A method of a type another group declares is exported, so the call
        # site reads the exported name. Only the job run file calls these.
        if os.path.abspath(own_group_folder) == os.path.abspath(
                os.path.join("internal", "t3-transcriptextraction-job", "run")):
            for old_method, new_method in (
                    ("addOk", "AddOk"), ("addFail", "AddFail"),
                    ("addInFlight", "AddInFlight")):
                body = re.sub(r"(?<![\w])" + old_method + r"\(",
                              new_method + "(", body)
        if os.path.abspath(own_group_folder) == os.path.abspath(
                os.path.join("cmd", "command")):
            subcommand_folder = os.path.join("cmd", "subcommand")
            for other in files:
                if os.path.abspath(os.path.dirname(other)) != os.path.abspath(subcommand_folder):
                    continue
                for declared_name in declarations_of(other):
                    body = re.sub(
                        r"(?<![\w.])(?!(?:func|type|var|const) )" + declared_name + r"\b",
                        "subcommand." + declared_name,
                        body,
                    )
            used_internal["subcommand"] = subcommand_folder
        # An import the body never names is dropped, so `go build` does not
        # report it as imported and not used.
        for package_name in list(used_internal):
            if not re.search(r"(?<![\w.])" + package_name + r"\.[A-Za-z_]",
                             body):
                del used_internal[package_name]
        lines = [f"package {own_package}", ""]
        if used_std or used_internal:
            lines.append("import (")
            for import_path in sorted(used_std):
                lines.append(f'\t"{import_path}"')
            if used_std and used_internal:
                lines.append("")
            for package_name in sorted(used_internal):
                folder = used_internal[package_name]
                relative = os.path.relpath(folder, ".")
                lines.append(f'\t"{MODULE}/{relative}"')
            lines.append(")")
            lines.append("")
        new_text = "\n".join(lines) + body.lstrip("\n")
        open(path, "w", encoding="utf-8").write(new_text)
    print(f"rewrote imports of {len(files)} files")


if __name__ == "__main__":
    # A second pass resolves a name whose package the first pass only learned
    # from a qualified use, such as `refpattern.TermPatternOf` teaching the
    # file that `TalkSpanListOf` lives in the same package.
    for pass_index in range(2):
        main()
