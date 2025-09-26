package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

func KoLoaded(name string) (bool, error) {
	staticList, err := loadKoList()
	if err != nil {
		return false, err
	}
	_, ok := staticList[name]
	return ok, nil
}

var (
	loaded   map[string]struct{}
	loadOnce sync.Once
	loadErr  error
)

func loadKoList() (map[string]struct{}, error) {
	loadOnce.Do(func() {
		f, err := os.Open("/proc/modules")
		if err != nil {
			loadErr = fmt.Errorf("cannot open /proc/modules: %w", err)
			return
		}
		defer f.Close()

		loaded = make(map[string]struct{})
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) == 0 {
				continue
			}
			loaded[fields[0]] = struct{}{}
		}
		loadErr = sc.Err()
	})
	return loaded, loadErr
}

func checkXenKos() error {
	essentials := []string{"xen_gntalloc", "xen_gntdev"}
	for i, ko := range essentials {
		loaded, err := KoLoaded(ko)
		if err != nil {
			return err
		}
		if !loaded {
			return fmt.Errorf("xl: %s is not loaded", essentials[i])
		}
	}
	return nil
}

func main() {
	fmt.Println("=== Xen Kernel Module Checker ===")
	fmt.Println("Checking required Xen kernel modules for mica-shim pedestal detection...")

	fmt.Printf("System: ")
	if hostname, err := os.Hostname(); err == nil {
		fmt.Printf("%s ", hostname)
	}
	if kernel, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.Split(string(kernel), " ")[2]
		fmt.Printf("(Linux %s)\n", version)
	} else {
		fmt.Println("Unknown")
	}

	fmt.Println("\nLoading kernel module list from /proc/modules...")
	modules, err := loadKoList()
	if err != nil {
		fmt.Printf("❌ Failed to load module list: %v\n", err)
		os.Exit(1)
	}

	totalModules := len(modules)
	fmt.Printf("✅ Found %d loaded kernel modules\n", totalModules)

	xenModules := []string{
		"xen_gntalloc",
		"xen_gntdev",
		"xen_netback",
		"xen_blkback",
		"xen_scsi_backend",
		"xen_pciback",
		"xen_netfront",
		"xen_blkfront",
		"xen_scsi_frontend",
		"xen_balloon",
		"xenfs",
		"xen_privcmd",
		"xen_acpi_processor",
		"xen_wdt",
		"xen_evtchn",
		"xen_xenbus",
	}

	fmt.Println("\nChecking Xen-related modules:")
	foundXenModules := 0
	for _, mod := range xenModules {
		if _, exists := modules[mod]; exists {
			fmt.Printf("  ✅ %s - LOADED\n", mod)
			foundXenModules++
		} else {
			fmt.Printf("  ❌ %s - NOT LOADED\n", mod)
		}
	}

	fmt.Printf("\nFound %d out of %d Xen-related modules\n", foundXenModules, len(xenModules))

	// Categorize findings
	essentialModules := []string{"xen_gntalloc", "xen_gntdev"}
	backendModules := []string{"xen_netback", "xen_blkback", "xen_scsi_backend", "xen_pciback"}
	frontendModules := []string{"xen_netfront", "xen_blkfront", "xen_scsi_frontend"}
	systemModules := []string{"xen_balloon", "xenfs", "xen_privcmd", "xen_acpi_processor", "xen_wdt", "xen_evtchn", "xen_xenbus"}

	fmt.Println("\n=== Module Categories ===")

	essentialCount := 0
	for _, mod := range essentialModules {
		if _, exists := modules[mod]; exists {
			essentialCount++
		}
	}
	fmt.Printf("Essential modules: %d/%d loaded\n", essentialCount, len(essentialModules))

	backendCount := 0
	for _, mod := range backendModules {
		if _, exists := modules[mod]; exists {
			backendCount++
		}
	}
	fmt.Printf("Backend modules: %d/%d loaded\n", backendCount, len(backendModules))

	frontendCount := 0
	for _, mod := range frontendModules {
		if _, exists := modules[mod]; exists {
			frontendCount++
		}
	}
	fmt.Printf("Frontend modules: %d/%d loaded\n", frontendCount, len(frontendModules))

	systemCount := 0
	for _, mod := range systemModules {
		if _, exists := modules[mod]; exists {
			systemCount++
		}
	}
	fmt.Printf("System modules: %d/%d loaded\n", systemCount, len(systemModules))

	fmt.Println("\n=== Checking Essential Xen Modules ===")
	essentials := []string{"xen_gntalloc", "xen_gntdev"}
	allLoaded := true

	for _, ko := range essentials {
		fmt.Printf("Checking %s... ", ko)
		loaded, err := KoLoaded(ko)
		if err != nil {
			fmt.Printf("❌ ERROR: %v\n", err)
			allLoaded = false
			continue
		}
		if loaded {
			fmt.Println("✅ LOADED")
		} else {
			fmt.Println("❌ NOT LOADED")
			allLoaded = false
		}
	}

	fmt.Println("\n=== Final Result ===")
	if allLoaded {
		fmt.Println("🎉 SUCCESS: All required Xen kernel modules are loaded!")
		fmt.Println("   Xen pedestal detection should work correctly.")
	} else {
		fmt.Println("❌ FAILURE: Some required Xen kernel modules are missing!")
		fmt.Println("   Xen pedestal detection will fail.")
		os.Exit(1)
	}
}
