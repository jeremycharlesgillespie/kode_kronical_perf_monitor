#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

# Variables
STRESS_PID=""
STRESS_RUNNING=false
CORES=$(nproc)
MIN_TEMP=999.0
MAX_TEMP=0.0
GRAPH_WIDTH=60
declare -a TEMP_HISTORY
declare -a CPU_HISTORY

# Initialize arrays
for ((i=0; i<$GRAPH_WIDTH; i++)); do
    TEMP_HISTORY[$i]=0
    CPU_HISTORY[$i]=0
done

# Function to start stress
start_stress() {
    if [ "$STRESS_RUNNING" = false ]; then
        stress --cpu $CORES &
        STRESS_PID=$!
        STRESS_RUNNING=true
    fi
}

# Function to stop stress
stop_stress() {
    if [ "$STRESS_RUNNING" = true ]; then
        kill $STRESS_PID 2>/dev/null
        STRESS_RUNNING=false
        STRESS_PID=""
    fi
}

# Function to update min/max temps
update_min_max() {
    local temp=$1
    # Remove the °C suffix and + prefix for comparison
    temp_val=$(echo $temp | sed 's/[+°C]//g')
    
    # Update min
    if (( $(echo "$temp_val < $MIN_TEMP" | bc -l) )); then
        MIN_TEMP=$temp_val
    fi
    
    # Update max
    if (( $(echo "$temp_val > $MAX_TEMP" | bc -l) )); then
        MAX_TEMP=$temp_val
    fi
}

# Function to get CPU usage percentage
get_cpu_usage() {
    # Get CPU usage from top command
    local usage=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}')
    if [ -z "$usage" ]; then
        # Alternative method using vmstat
        usage=$(vmstat 1 2 | tail -1 | awk '{print 100 - $15}')
    fi
    echo $usage
}

# Function to get individual CPU core usage
get_cpu_cores_usage() {
    # Get per-core CPU usage using mpstat, excluding the "all" line
    mpstat -P ALL 1 1 2>/dev/null | grep -E "^Average:[[:space:]]+[0-9]+" | awk '{print 100-$NF}'
}

# Function to get color based on usage percentage
get_usage_color() {
    local usage=$1
    if (( $(echo "$usage >= 90" | bc -l) )); then
        echo -e "\033[1;31m"  # Bright red
    elif (( $(echo "$usage >= 80" | bc -l) )); then
        echo -e "\033[0;31m"  # Red
    elif (( $(echo "$usage >= 70" | bc -l) )); then
        echo -e "\033[38;5;208m"  # Orange
    elif (( $(echo "$usage >= 60" | bc -l) )); then
        echo -e "\033[1;33m"  # Yellow
    elif (( $(echo "$usage >= 50" | bc -l) )); then
        echo -e "\033[0;33m"  # Dark yellow
    elif (( $(echo "$usage >= 40" | bc -l) )); then
        echo -e "\033[0;32m"  # Green
    elif (( $(echo "$usage >= 30" | bc -l) )); then
        echo -e "\033[0;36m"  # Cyan
    elif (( $(echo "$usage >= 20" | bc -l) )); then
        echo -e "\033[38;5;39m"  # Light blue
    elif (( $(echo "$usage >= 10" | bc -l) )); then
        echo -e "\033[0;34m"  # Blue
    else
        echo -e "\033[38;5;17m"  # Dark blue
    fi
}

# Function to calculate grid dimensions
get_grid_dimensions() {
    local count=$1
    local cols rows
    
    case $count in
        1|2|3|4) cols=2; rows=$(( (count + 1) / 2 )) ;;
        5|6) cols=3; rows=2 ;;
        7|8) cols=4; rows=2 ;;
        9|10|11|12) cols=4; rows=3 ;;
        13|14|15|16) cols=4; rows=4 ;;
        17|18|19|20) cols=5; rows=4 ;;
        21|22|23|24|25) cols=5; rows=5 ;;
        26|27|28|29|30) cols=6; rows=5 ;;
        31|32|33|34|35|36) cols=6; rows=6 ;;
        *) cols=$(echo "sqrt($count) + 1" | bc); rows=$cols ;;
    esac
    
    echo "$cols $rows"
}

# Function to display CPU cores as colored blocks
display_cpu_cores() {
    local cores_usage=($(get_cpu_cores_usage))
    local core_count=${#cores_usage[@]}
    
    # Get grid dimensions
    local dims=($(get_grid_dimensions $core_count))
    local cols=${dims[0]}
    local rows=${dims[1]}
    
    echo -e "${CYAN}CPU Cores (${core_count} cores):${NC}"
    
    # Display cores in grid
    for ((row=0; row<$rows; row++)); do
        local row_start=$((row * cols))
        echo -n "  "
        
        # Display blocks for this row
        for ((col=0; col<$cols; col++)); do
            local core_idx=$((row_start + col))
            if [ $core_idx -lt $core_count ]; then
                local usage=${cores_usage[$core_idx]}
                local color=$(get_usage_color "$usage")
                echo -en "${color}█${NC}"
            else
                echo -n " "
            fi
        done
        
        # Display percentages for this row
        echo -n "  "
        for ((col=0; col<$cols; col++)); do
            local core_idx=$((row_start + col))
            if [ $core_idx -lt $core_count ]; then
                local usage=${cores_usage[$core_idx]}
                printf "%5.1f%%" "$usage"
            else
                echo -n "      "
            fi
            if [ $col -lt $((cols-1)) ]; then
                echo -n " "
            fi
        done
        
        echo "                    "
    done
}

# Function to draw graph
draw_graph() {
    local -n array=$1
    local title=$2
    local unit=$3
    local current_val=$4
    
    echo -e "${CYAN}$title${NC} Current: ${YELLOW}$current_val$unit${NC}                    "
    
    # Draw 5 rows (top to bottom: 81-100, 61-80, 41-60, 21-40, 0-20)
    for row in {4..0}; do
        case $row in
            4) range="81-100"; color=$RED ;;
            3) range="61-80 "; color=$YELLOW ;;
            2) range="41-60 "; color=$GREEN ;;
            1) range="21-40 "; color=$CYAN ;;
            0) range="0-20  "; color=$BLUE ;;
        esac
        
        echo -en "${color}$range |${NC}"
        
        for ((i=0; i<$GRAPH_WIDTH; i++)); do
            val=${array[$i]}
            # Determine which row this value belongs to
            if [ $row -eq 4 ] && (( $(echo "$val > 80" | bc -l) )); then
                echo -n "█"
            elif [ $row -eq 3 ] && (( $(echo "$val > 60 && $val <= 80" | bc -l) )); then
                echo -n "█"
            elif [ $row -eq 2 ] && (( $(echo "$val > 40 && $val <= 60" | bc -l) )); then
                echo -n "█"
            elif [ $row -eq 1 ] && (( $(echo "$val > 20 && $val <= 40" | bc -l) )); then
                echo -n "█"
            elif [ $row -eq 0 ] && (( $(echo "$val >= 0 && $val <= 20" | bc -l) )); then
                echo -n "█"
            else
                echo -n " "
            fi
        done
        echo "  "  # Add spaces to clear any leftover characters
    done
    echo -n "       └"
    for ((i=0; i<$GRAPH_WIDTH; i++)); do
        echo -n "─"
    done
    echo "  "  # Add spaces to clear any leftover characters
}

# Function to shift array and add new value
shift_array() {
    local -n array=$1
    local new_val=$2
    
    # Shift all values left
    for ((i=0; i<$((GRAPH_WIDTH-1)); i++)); do
        array[$i]=${array[$((i+1))]}
    done
    # Add new value at the end
    array[$((GRAPH_WIDTH-1))]=$new_val
}

# Cleanup on exit
cleanup() {
    stop_stress
    # Reset terminal settings
    stty echo
    tput cnorm  # Show cursor
    echo -e "\n${RED}Exiting...${NC}"
    exit
}

# Trap Ctrl+C and other signals
trap cleanup INT TERM EXIT

# Clear screen
clear

echo -e "${GREEN}=== CPU Stress Test with Rolling Graphs ===${NC}"
echo -e "${YELLOW}Press SPACEBAR to toggle stress test ON/OFF${NC}"
echo -e "${YELLOW}Press Ctrl+C to exit${NC}\n"
echo -e "Detected ${GREEN}$CORES${NC} CPU cores\n"

# Configure terminal for non-blocking input
stty -echo -icanon time 0 min 0
tput civis  # Hide cursor

# Main loop
while true; do
    # Check for spacebar press (ASCII 32)
    key=$(dd bs=1 count=1 2>/dev/null | od -An -N1 -i | tr -d ' ')
    
    if [ "$key" = "32" ]; then
        if [ "$STRESS_RUNNING" = true ]; then
            stop_stress
        else
            start_stress
        fi
    fi
    
    # Get CPU temp
    TEMP=$(sensors k10temp-pci-00c3 2>/dev/null | grep "Tctl:" | awk '{print $2}')
    temp_val=$(echo $TEMP | sed 's/[+°C]//g')
    
    # Get CPU usage
    cpu_usage=$(get_cpu_usage)
    
    # Update min/max
    if [ ! -z "$TEMP" ]; then
        update_min_max "$TEMP"
    fi
    
    # Update history arrays
    if [ ! -z "$temp_val" ]; then
        shift_array TEMP_HISTORY "$temp_val"
    fi
    if [ ! -z "$cpu_usage" ]; then
        shift_array CPU_HISTORY "$cpu_usage"
    fi
    
    # Move cursor to top-left instead of clearing
    tput cup 0 0
    
    # Display status
    if [ "$STRESS_RUNNING" = true ]; then
        STATUS="${RED}[STRESS ON]${NC}"
    else
        STATUS="${GREEN}[STRESS OFF]${NC}"
    fi
    
    echo -e "${GREEN}=== CPU Stress Test with Rolling Graphs ===${NC}                    "
    echo -e "${YELLOW}Press SPACEBAR to toggle stress test ON/OFF${NC}                    "
    echo -e "${YELLOW}Press Ctrl+C to exit${NC}                                          "
    echo -e ""
    echo -e "Status: $STATUS  ${BLUE}Min Temp:${NC} ${GREEN}+${MIN_TEMP}°C${NC}  ${BLUE}Max Temp:${NC} ${RED}+${MAX_TEMP}°C${NC}          "
    echo -e ""
    
    # Display CPU cores
    display_cpu_cores
    echo -e ""
    
    # Draw temperature graph
    draw_graph TEMP_HISTORY "Temperature Graph" "" "$TEMP"
    echo
    
    # Draw CPU usage graph
    draw_graph CPU_HISTORY "CPU Usage Graph" "%" "${cpu_usage}"
    
    sleep 0.5
done