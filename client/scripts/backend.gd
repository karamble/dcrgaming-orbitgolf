extends Node
signal reply(message: Dictionary)

var pipe: FileAccess
var pid := -1
var reader := Thread.new()
var mutex := Mutex.new()
var inbox: Array[Dictionary] = []
var sequence := 0
var requests: Dictionary = {}

func _ready() -> void:
	var executable := ProjectSettings.globalize_path("res://../bin/orbit-backend")
	if OS.has_feature("windows"):
		executable += ".exe"
	var args := PackedStringArray()
	var user_args := OS.get_cmdline_user_args()
	for i in range(user_args.size() - 1):
		if user_args[i] == "--appdata":
			args.append_array(["--appdata", user_args[i + 1]])
	var process := OS.execute_with_pipe(executable, args, true)
	if process.is_empty():
		call_deferred("emit_signal", "reply", {"ok": false, "error": "Go backend missing. Run make build first."})
		return
	pipe = process.stdio
	pid = process.pid
	reader.start(_read)

func send(method: String, data: Dictionary = {}) -> int:
	if pipe == null:
		return -1
	sequence += 1
	data["method"] = method
	data["id"] = sequence
	requests[sequence]=method
	pipe.store_string(JSON.stringify(data) + "\n")
	pipe.flush()
	return sequence

func _read() -> void:
	while true:
		var line := pipe.get_line()
		if line.is_empty():
			break
		var value = JSON.parse_string(line)
		if value is Dictionary:
			mutex.lock()
			inbox.append(value)
			mutex.unlock()

func _process(_delta: float) -> void:
	mutex.lock()
	var batch := inbox.duplicate()
	inbox.clear()
	mutex.unlock()
	for item in batch:
		var id := int(item.get("id",0))
		item["method"]=requests.get(id,"")
		requests.erase(id)
		reply.emit(item)

func _exit_tree() -> void:
	if pid > 0:
		send("quit")
		# Let the backend cancel in-flight work and close its journals normally.
		var deadline := Time.get_ticks_msec()+3000
		while OS.is_process_running(pid) and Time.get_ticks_msec()<deadline:
			OS.delay_msec(10)
		if OS.is_process_running(pid): OS.kill(pid)
	if reader.is_started():
		reader.wait_to_finish()
