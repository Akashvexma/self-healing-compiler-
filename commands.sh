#!/bin/zsh
# Quick commands to run the self-healing compiler pipeline

# Color codes
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

PROJECT_DIR="/Users/akashverma/Downloads/self-healing-llm-pipeline/self-healing-compiler-"

echo -e "${BLUE}===================================${NC}"
echo -e "${BLUE}Self-Healing Compiler Pipeline${NC}"
echo -e "${BLUE}===================================${NC}\n"

# Check if Ollama is running
check_ollama() {
    if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Ollama is running${NC}"
        return 0
    else
        echo -e "${YELLOW}⚠ Ollama not responding on localhost:11434${NC}"
        echo "Start Ollama with: ollama serve"
        return 1
    fi
}

# List available commands
show_help() {
    cat << 'EOF'

Usage: source commands.sh
Then use one of:

  run_server          - Start the web server (port 8080)
  check_ollama        - Check if Ollama is running
  list_models         - List available Ollama models
  pull_model <name>   - Download a model (e.g., "llama3.2:1b")
  open_browser        - Open http://localhost:8080 in browser
  
  build               - Build the project
  test                - Run tests
  clean               - Clean build artifacts

Examples:
  pull_model llama3.2:1b
  pull_model deepseek-r1:8b

EOF
}

# Run the web server
run_server() {
    echo -e "${BLUE}Starting web server...${NC}"
    cd "$PROJECT_DIR"
    
    if ! check_ollama; then
        echo -e "${YELLOW}Warning: Ollama not running. Server will fail to generate code.${NC}"
        read -q "yn?Continue anyway? (y/n) " && echo
        [[ $yn == "n" ]] && return 1
    fi
    
    echo -e "${GREEN}Starting on http://localhost:8080${NC}"
    go run main.go
}

# List available models
list_models() {
    echo -e "${BLUE}Available Ollama models:${NC}"
    curl -s http://localhost:11434/api/tags | grep -o '"name":"[^"]*' | cut -d'"' -f4
}

# Download a model
pull_model() {
    if [ -z "$1" ]; then
        echo "Usage: pull_model <model_name>"
        echo "Example: pull_model llama3.2:1b"
        return 1
    fi
    
    echo -e "${BLUE}Downloading model: $1${NC}"
    ollama pull "$1"
}

# Open browser
open_browser() {
    echo -e "${BLUE}Opening http://localhost:8080${NC}"
    open "http://localhost:8080"
}

# Build project
build() {
    echo -e "${BLUE}Building project...${NC}"
    cd "$PROJECT_DIR"
    go build -o compiler-pipeline main.go
    echo -e "${GREEN}✓ Build complete: ./compiler-pipeline${NC}"
}

# Run tests
test() {
    echo -e "${BLUE}Running tests...${NC}"
    cd "$PROJECT_DIR"
    go test ./...
}

# Clean
clean() {
    echo -e "${BLUE}Cleaning...${NC}"
    cd "$PROJECT_DIR"
    go clean
    rm -f compiler-pipeline
    echo -e "${GREEN}✓ Clean complete${NC}"
}

# Show help if requested
if [ "$1" == "-h" ] || [ "$1" == "--help" ] || [ "$1" == "help" ]; then
    show_help
    exit 0
fi

# Show welcome
echo -e "${YELLOW}Quick start:${NC}"
echo "  1. Source this file: source commands.sh"
echo "  2. Check Ollama: check_ollama"
echo "  3. Start server: run_server"
echo "  4. Open browser: open_browser"
echo ""
echo "For all commands: show_help"
echo ""

# Auto-check Ollama
check_ollama

