package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const suiteVersionFilename = "suite-version.json"

type SuiteVersion struct {
	Schema         int    `json:"schema"`
	ReleaseTag     string `json:"releaseTag"`
	CPAVersion     string `json:"cpaVersion"`
	ManagerVersion string `json:"managerVersion"`
}

func ReadSuiteVersion(root string) (SuiteVersion, error) {
	path := filepath.Join(root, suiteVersionFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return SuiteVersion{}, fmt.Errorf("read installed suite version: %w", err)
	}
	var version SuiteVersion
	if err := json.Unmarshal(data, &version); err != nil {
		return SuiteVersion{}, fmt.Errorf("decode installed suite version: %w", err)
	}
	if version.Schema != 1 || strings.TrimSpace(version.CPAVersion) == "" || strings.TrimSpace(version.ManagerVersion) == "" {
		return SuiteVersion{}, errors.New("installed suite version metadata is incomplete")
	}
	return version, nil
}

func WriteSuiteVersion(root string, version SuiteVersion) error {
	version.Schema = 1
	version.CPAVersion = strings.TrimSpace(version.CPAVersion)
	version.ManagerVersion = strings.TrimSpace(version.ManagerVersion)
	version.ReleaseTag = strings.TrimSpace(version.ReleaseTag)
	if version.CPAVersion == "" || version.ManagerVersion == "" {
		return errors.New("suite version metadata requires CPA and CPA-Manager versions")
	}
	data, err := json.MarshalIndent(version, "", "  ")
	if err != nil {
		return fmt.Errorf("encode suite version: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, suiteVersionFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create suite version directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write suite version: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit suite version: %w", err)
	}
	return nil
}

// CompareVersions compares release versions using numeric components and
// prerelease ordering. It returns false when either value is not a version.
func CompareVersions(left, right string) (int, bool) {
	leftParts, ok := parseVersion(left)
	if !ok {
		return 0, false
	}
	rightParts, ok := parseVersion(right)
	if !ok {
		return 0, false
	}
	length := len(leftParts.numbers)
	if len(rightParts.numbers) > length {
		length = len(rightParts.numbers)
	}
	for i := 0; i < length; i++ {
		var leftNumber, rightNumber uint64
		if i < len(leftParts.numbers) {
			leftNumber = leftParts.numbers[i]
		}
		if i < len(rightParts.numbers) {
			rightNumber = rightParts.numbers[i]
		}
		if leftNumber > rightNumber {
			return 1, true
		}
		if leftNumber < rightNumber {
			return -1, true
		}
	}
	if len(leftParts.prerelease) == 0 && len(rightParts.prerelease) == 0 {
		return 0, true
	}
	if len(leftParts.prerelease) == 0 {
		return 1, true
	}
	if len(rightParts.prerelease) == 0 {
		return -1, true
	}
	for i := 0; i < len(leftParts.prerelease) && i < len(rightParts.prerelease); i++ {
		leftID, rightID := leftParts.prerelease[i], rightParts.prerelease[i]
		leftNumber, leftNumeric := numericIdentifier(leftID)
		rightNumber, rightNumeric := numericIdentifier(rightID)
		switch {
		case leftNumeric && rightNumeric:
			if leftNumber > rightNumber {
				return 1, true
			}
			if leftNumber < rightNumber {
				return -1, true
			}
		case leftNumeric:
			return -1, true
		case rightNumeric:
			return 1, true
		case leftID > rightID:
			return 1, true
		case leftID < rightID:
			return -1, true
		}
	}
	if len(leftParts.prerelease) > len(rightParts.prerelease) {
		return 1, true
	}
	if len(leftParts.prerelease) < len(rightParts.prerelease) {
		return -1, true
	}
	return 0, true
}

type parsedVersion struct {
	numbers    []uint64
	prerelease []string
}

func parseVersion(value string) (parsedVersion, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V")
	value, _, _ = strings.Cut(value, "+")
	base, prerelease, hasPrerelease := strings.Cut(value, "-")
	if base == "" {
		return parsedVersion{}, false
	}
	parts := strings.Split(base, ".")
	parsed := parsedVersion{numbers: make([]uint64, 0, len(parts))}
	for _, part := range parts {
		if part == "" {
			return parsedVersion{}, false
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		parsed.numbers = append(parsed.numbers, number)
	}
	if hasPrerelease {
		if prerelease == "" {
			return parsedVersion{}, false
		}
		parsed.prerelease = strings.Split(prerelease, ".")
		for _, identifier := range parsed.prerelease {
			if identifier == "" {
				return parsedVersion{}, false
			}
		}
	}
	return parsed, true
}

func numericIdentifier(identifier string) (uint64, bool) {
	number, err := strconv.ParseUint(identifier, 10, 64)
	return number, err == nil
}

func HasNewerSuiteVersion(latest Manifest, current SuiteVersion) (bool, error) {
	cpaComparison, ok := CompareVersions(latest.CPAVersion, current.CPAVersion)
	if !ok {
		return false, fmt.Errorf("cannot compare CLIProxyAPI versions %q and %q", latest.CPAVersion, current.CPAVersion)
	}
	managerComparison, ok := CompareVersions(latest.ManagerVersion, current.ManagerVersion)
	if !ok {
		return false, fmt.Errorf("cannot compare CPA-Manager versions %q and %q", latest.ManagerVersion, current.ManagerVersion)
	}
	return cpaComparison > 0 || managerComparison > 0, nil
}
