/*
 * Minio Cloud Storage, (C) 2016-2017 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func as(instructions []Instruction) error {
	for i := range instructions {
		ins := instructions[i]

		assembled, opcodes, err := asSingle(ins.instruction, ins.lineno, ins.commentPos, ins.inDefine)
		if err != nil {
			return err
		}
		instructions[i].assembled = assembled
		instructions[i].opcodes = make([]byte, len(opcodes))
		copy(instructions[i].opcodes[:], opcodes)
	}
	return nil
}

func asSingle(instr string, lineno, commentPos int, inDefine bool) (string, []byte, error) {

	instrFields := strings.Split(instr, "/*")
	content := []byte(instrFields[0] + "\n")
	tmpfile, err := ioutil.TempFile("", "asm2plan9s")
	if err != nil {
		return "", nil, err
	}

	if _, err := tmpfile.Write(content); err != nil {
		return "", nil, err
	}
	if err := tmpfile.Close(); err != nil {
		return "", nil, err
	}

	asmFile := tmpfile.Name() + ".asm"
	lisFile := tmpfile.Name() + ".lis"
	objFile := tmpfile.Name() + ".obj"
	os.Rename(tmpfile.Name(), asmFile)

	defer os.Remove(asmFile) // clean up
	defer os.Remove(lisFile) // clean up
	defer os.Remove(objFile) // clean up

	// /opt/riscv/bin/riscv64-unknown-elf-as -aln=first.lis -march=rv64gv -o first.o first.s
	app := "/opt/riscv/bin/riscv64-unknown-elf-as"

	arg0 := "-march=rv64gv"
	arg1 := "-o"
	arg2 := objFile
	arg3 := fmt.Sprintf("-aln=%s", lisFile)
	arg4 := asmFile

	cmd := exec.Command(app, arg0, arg1, arg2, arg3, arg4)
	cmb, err := cmd.CombinedOutput()
	if err != nil {
		asmErrs := strings.Split(string(cmb)[len(asmFile)+1:], ":")
		asmErr := strings.Join(asmErrs[1:], ":")
		return "", nil, errors.New(fmt.Sprintf("AS error (line %d for '%s'):", lineno+1, strings.TrimSpace(instr)) + asmErr)
	}

	return toPlan9sArm(lisFile, instr)
}

func toPlan9sArm(listFile, instr string) (string, []byte, error) {

	var r = regexp.MustCompile(`^\s+\d+\s+\d+\s+([0-9a-fA-F]+)`)

	outputLines, err := readLines(listFile, nil)
	if err != nil {
		return "", nil, err
	}

	lastLine := outputLines[len(outputLines)-1]
	if strings.Contains(lastLine, "Warning") {
		fmt.Println("*** Encountered Warning (dropping last line)")
		fmt.Println(strings.Repeat("=", 44))
		fmt.Println(strings.Join(outputLines, "\n"))
		fmt.Println(strings.Repeat("=", 44))

		// drop last line (so instruction opcodes are the last lines)
		outputLines = outputLines[:len(outputLines)-1]
	}

	sline := "    "

	if match := r.FindStringSubmatch(lastLine); len(match) > 1 {
		sline += fmt.Sprintf("WORD $0x%s%s%s%s", strings.ToLower(match[1][6:8]), strings.ToLower(match[1][4:6]), strings.ToLower(match[1][2:4]), strings.ToLower(match[1][0:2]))
	} else {
		return "", nil, errors.New("regexp failed")
	}

	sline += " //" + instr

	return sline, nil, nil
}
