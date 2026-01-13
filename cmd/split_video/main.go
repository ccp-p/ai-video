package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// 硬编码配置
	inputFile := `D:\download\2.mp4`
	splitTime := "01:05:00" // 格式为 HH:MM:SS
	output1 := `D:\download\2_part1.mp4`
	output2 := `D:\download\2_part2.mp4`
	output3 := `D:\download\2_part3.mp4`

	fmt.Printf("正在分割视频: %s\n分割点: %s\n", inputFile, splitTime)

	// 1. 提取前半部分 (0 到 65分钟)
	cmd1 := exec.Command("ffmpeg", "-i", inputFile, "-t", splitTime, "-c", "copy", "-y", output1)
	if err := cmd1.Run(); err != nil {
		fmt.Printf("处理前半部分出错: %v\n", err)
		return
	}
	fmt.Println("完成第一部分:", output1)
	
	// 2. 提取第二部分 (65分钟 到 120分钟)
	cmd2 := exec.Command("ffmpeg", "-i", inputFile, "-ss", splitTime, "-t", "00:55:00", "-c", "copy", "-y", output2)
	if err := cmd2.Run(); err != nil {
		fmt.Printf("处理第二部分出错: %v\n", err)
		return
	}
	fmt.Println("完成第二部分:", output2)
	// 3. 提取第三部分 (120分钟 到 结束)
	cmd3 := exec.Command("ffmpeg", "-i", inputFile, "-ss", "02:00:00", "-c", "copy", "-y", output3)
	if err := cmd3.Run(); err != nil {
		fmt.Printf("处理第三部分出错: %v\n", err)
		return
	}

	fmt.Println("完成第三部分:", output3)

	fmt.Println("视频分割任务已成功完成。")
}
