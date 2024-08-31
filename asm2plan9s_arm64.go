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

	// First to yasm (will return error when not installed)
	e := yasm(instructions)
	if e == nil {
		return e
	}
	// Try gas if yasm not installed
	return gas(instructions)
}

func gas(instructions []Instruction) error {
	for i := range instructions {
		ins := instructions[i]
		next := ""

		// for the `movprfx` instruction, pass in the next line
		// (since this instruction will be checked against it)
		if strings.Contains(ins.instruction, "movprfx") && i < len(instructions)-1 {
			next = strings.Split(instructions[i+1].instruction, "/*")[0] + "\n"
		}

		assembled, opcodes, err := asSingle(ins.instruction, next, ins.lineno, ins.commentPos, ins.inDefine)
		if err != nil {
			return err
		}
		instructions[i].assembled = assembled
		instructions[i].opcodes = make([]byte, len(opcodes))
		copy(instructions[i].opcodes[:], opcodes)
	}
	return nil
}

func asSingle(instr, next string, lineno, commentPos int, inDefine bool) (string, []byte, error) {

	instrFields := strings.Split(instr, "/*")
	content := []byte(instrFields[0] + "\n" + next)
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

	// as -march=armv9-a+crypto -o first.out -al=first.lis first.s
	app := "as"

	arg0 := "-march=armv9-a+sve+sve2+sve2-bitperm" // See https://sourceware.org/binutils/docs/as/AArch64-Extensions.html
	arg1 := "-o"
	arg2 := objFile
	arg3 := fmt.Sprintf("-al=%s", lisFile)
	arg4 := asmFile

	cmd := exec.Command(app, arg0, arg1, arg2, arg3, arg4)
	cmb, err := cmd.CombinedOutput()
	if err != nil {
		asmErrs := strings.Split(string(cmb)[len(asmFile)+1:], ":")
		asmErr := strings.Join(asmErrs[1:], ":")
		return "", nil, errors.New(fmt.Sprintf("GAS error (line %d for '%s'):", lineno+1, strings.TrimSpace(instr)) + asmErr)
	}

	return toPlan9sArm(lisFile, instr, next != "")
}

func toPlan9sArm(listFile, instr string, hasNext bool) (string, []byte, error) {

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
	if hasNext {
		if strings.Contains(outputLines[len(outputLines)-2], "1 0000") &&
			strings.Contains(outputLines[len(outputLines)-1], "2 0004") {
			lastLine = outputLines[len(outputLines)-2]
		} else {
			fmt.Println(strings.Join(outputLines, "\n"))
			panic("Unexpected")
		}
	}

	sline := "    "

	if match := r.FindStringSubmatch(lastLine); len(match) > 1 {
		sline += fmt.Sprintf("WORD $0x%s%s%s%s", strings.ToLower(match[1][6:8]), strings.ToLower(match[1][4:6]), strings.ToLower(match[1][2:4]), strings.ToLower(match[1][0:2]))
	} else {
		return "", nil, errors.New("regexp failed")
	}

	sline += " //" + instr

	// fmt.Println(sline)

	return sline, nil, nil
}
