package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectTemplate defines the file structure for a new project.
type ProjectTemplate struct {
	Files map[string]string // file path -> content template
}

// languageTemplates contains built-in templates for each supported language.
var languageTemplates = map[string]ProjectTemplate{
	"go": {
		Files: map[string]string{
			"go.mod": `module {{.ProjectName}}

go 1.21
`,
			"main.go": `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`,
		},
	},

	"javascript": {
		Files: map[string]string{
			"package.json": `{
  "name": "{{.ProjectName}}",
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "start": "node index.js"
  }
}
`,
			"index.js": `console.log("Hello, World!");
`,
		},
	},

	"typescript": {
		Files: map[string]string{
			"package.json": `{
  "name": "{{.ProjectName}}",
  "version": "1.0.0",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}
`,
			"tsconfig.json": `{
  "compilerOptions": {
    "target": "ES2020",
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true
  },
  "include": ["src/**/*"]
}
`,
			"src/index.ts": `console.log("Hello, World!");
`,
		},
	},

	"tsx": {
		Files: map[string]string{
			"package.json": `{
  "name": "{{.ProjectName}}",
  "version": "1.0.0",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "devDependencies": {
    "typescript": "^5.0.0",
    "vite": "^5.0.0",
    "react": "^18.0.0",
    "react-dom": "^18.0.0",
    "@types/react": "^18.0.0",
    "@types/react-dom": "^18.0.0"
  }
}
`,
			"tsconfig.json": `{
  "compilerOptions": {
    "target": "ES2020",
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true
  }
}
`,
			"src/main.tsx": `import React from 'react';
import ReactDOM from 'react-dom/client';

function App() {
  return <h1>Hello, World!</h1>;
}

ReactDOM.createRoot(document.getElementById('root')!).render(<App />);
`,
			"index.html": `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>{{.ProjectName}}</title>
</head>
<body>
  <div id="root"></div>
  <script type="module" src="/src/main.tsx"></script>
</body>
</html>
`,
		},
	},

	"python": {
		Files: map[string]string{
			"requirements.txt": ``,
			"main.py": `def main():
    print("Hello, World!")

if __name__ == "__main__":
    main()
`,
		},
	},

	"rust": {
		Files: map[string]string{
			"Cargo.toml": `[package]
name = "{{.ProjectName}}"
version = "0.1.0"
edition = "2021"

[dependencies]
`,
			"src/main.rs": `fn main() {
    println!("Hello, World!");
}
`,
		},
	},

	"java": {
		Files: map[string]string{
			"pom.xml": `<?xml version="1.0" encoding="UTF-8"?>
<project>
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.example</groupId>
    <artifactId>{{.ProjectName}}</artifactId>
    <version>1.0-SNAPSHOT</version>
    <properties>
        <maven.compiler.source>17</maven.compiler.source>
        <maven.compiler.target>17</maven.compiler.target>
    </properties>
</project>
`,
			"src/main/java/Main.java": `public class Main {
    public static void main(String[] args) {
        System.out.println("Hello, World!");
    }
}
`,
		},
	},

	"csharp": {
		Files: map[string]string{
			"{{.ProjectName}}.csproj": `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>
`,
			"Program.cs": `Console.WriteLine("Hello, World!");
`,
		},
	},

	"c": {
		Files: map[string]string{
			"main.c": `#include <stdio.h>

int main() {
    printf("Hello, World!\n");
    return 0;
}
`,
			"Makefile": `CC = gcc
CFLAGS = -Wall -Wextra

all: main

main: main.c
	$(CC) $(CFLAGS) -o main main.c

clean:
	rm -f main
`,
		},
	},

	"cpp": {
		Files: map[string]string{
			"main.cpp": `#include <iostream>

int main() {
    std::cout << "Hello, World!" << std::endl;
    return 0;
}
`,
			"CMakeLists.txt": `cmake_minimum_required(VERSION 3.10)
project({{.ProjectName}})

set(CMAKE_CXX_STANDARD 17)

add_executable(main main.cpp)
`,
		},
	},
}

// InitLanguageFiles creates initial files for a new project based on language.
func InitLanguageFiles(projectDir, language, projectName string) error {
	template, ok := languageTemplates[language]
	if !ok {
		// Unknown language, create empty directory
		return nil
	}

	// Variables for template substitution
	vars := map[string]string{
		"ProjectName": projectName,
	}

	for filePath, content := range template.Files {
		// Substitute variables in both path and content
		for k, v := range vars {
			placeholder := "{{." + k + "}}"
			content = strings.ReplaceAll(content, placeholder, v)
			filePath = strings.ReplaceAll(filePath, placeholder, v)
		}

		// Create full path
		fullPath := filepath.Join(projectDir, filePath)
		dir := filepath.Dir(fullPath)

		// Ensure directory exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}

		// Write file
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write file %s: %w", fullPath, err)
		}
	}

	return nil
}

// GetSupportedLanguages returns the list of supported language IDs.
func GetSupportedLanguages() []string {
	languages := make([]string, 0, len(languageTemplates))
	for lang := range languageTemplates {
		languages = append(languages, lang)
	}
	return languages
}