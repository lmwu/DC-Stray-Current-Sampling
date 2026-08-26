module OpcUaServerApp

using Open62541
using Random
using Printf

# ==============================================================================
# 🛠️ 第一部分：記憶體與 open62541 C API 綁定修復
# ==============================================================================

# 強制寫入 Julia 不可變 C 結構體 (immutable struct) 的內部欄位
function set_cfield!(ref::Ref{T}, field::Symbol, val) where T
    ptr = Base.unsafe_convert(Ptr{T}, ref)
    idx = findfirst(==(field), fieldnames(T))
    if idx === nothing
        error("在型態 $T 中找不到欄位 :$field")
    end
    off = fieldoffset(T, idx)
    ftype = fieldtype(T, idx)
    unsafe_store!(Ptr{ftype}(ptr + off), convert(ftype, val))
    return ref
end

# C 語言結構體指標轉型
function to_node_attr(attr_ref::Ref{UA_VariableAttributes})
    ptr_var = Base.unsafe_convert(Ptr{UA_VariableAttributes}, attr_ref)
    return Ptr{UA_NodeAttributes}(ptr_var)
end

# 安全且不造成記憶體洩漏的 Double 寫入函式
function write_double_node(server::JUA_Server, nodeId::UA_NodeId, val::Float64)
    v = Ref{UA_Variant}()
    
    # 關鍵修正 1：指標加法必須乘以 sizeof(UA_DataType)
    type_double_ptr = UA_TYPES[] + UA_TYPES_DOUBLE * sizeof(UA_DataType)
    type_variant_ptr = UA_TYPES[] + UA_TYPES_VARIANT * sizeof(UA_DataType)
    
    # 1. 將 Float64 數值封裝入 UA_Variant
    UA_Variant_setScalarCopy(v, Ref(val), type_double_ptr)
    
    # 2. 寫入 OPC UA 伺服器
    status = Open62541.__UA_Server_write(
        server,
        Ref(nodeId),
        UA_ATTRIBUTEID_VALUE,
        type_variant_ptr,
        v
    )
    
    # 3. 轉為 Ptr{UA_Variant} 指標並釋放 C 語言 Variant 記憶體
    v_ptr = Base.unsafe_convert(Ptr{UA_Variant}, v)
    UA_Variant_clear(v_ptr)
    
    return status
end

# ==============================================================================
# 📊 第二部分：模擬器內部資料結構
# ==============================================================================

mutable struct Station
    name::String
    pot_node_id::UA_NodeId
    cur_node_id::UA_NodeId
    pot_val::Float64
    cur_val::Float64
end

# ==============================================================================
# 🚀 第三部分：主程式核心邏輯
# ==============================================================================

function run_server()
    println("==================================================")
    println("  🚀 Julia + open62541 OPC UA 模擬伺服器啟動中...")
    println("==================================================")

    # 初始化 Server
    server = JUA_Server()
    config = JUA_ServerConfig(server)
    JUA_ServerConfig_setDefault(config)

    stations = Station[]

    parent_node_id = UA_NODEID_NUMERIC(0, UA_NS0ID_OBJECTSFOLDER)
    parent_ref_type_id = UA_NODEID_NUMERIC(0, UA_NS0ID_ORGANIZES)
    variable_type_id = UA_NODEID_NUMERIC(0, UA_NS0ID_BASEDATAVARIABLETYPE)

    # 關鍵修正 2：指標加法必須乘以 sizeof(UA_DataType)
    type_double_ptr = UA_TYPES[] + UA_TYPES_DOUBLE * sizeof(UA_DataType)
    double_type_node_id = UA_NODEID_NUMERIC(0, UA_NS0ID_DOUBLE)

    println("正在建構 100 個腐蝕測站...")

    for i in 1:100
        st_name = @sprintf("Station_%03d", i)

        base_pot = -0.850 - rand() * 0.300
        base_cur = 10.0 + rand() * 40.0

        # --- 1. DCPotential 節點 ---
        pot_str = "$(st_name).DCPotential"
        pot_node_id = UA_NODEID_STRING_ALLOC(1, pot_str)
        attr_pot = Ref(UA_VariableAttributes_default[])
        set_cfield!(attr_pot, :displayName, UA_LOCALIZEDTEXT("en-US", "DCPotential"))
        set_cfield!(attr_pot, :dataType, double_type_node_id)

        v_pot_init = Ref{UA_Variant}()
        UA_Variant_setScalarCopy(v_pot_init, Ref(base_pot), type_double_ptr)
        set_cfield!(attr_pot, :value, v_pot_init[])

        qn_pot = UA_QUALIFIEDNAME_ALLOC(1, pot_str)
        UA_Server_addVariableNode(
            server, pot_node_id, parent_node_id, parent_ref_type_id,
            qn_pot, variable_type_id,
            to_node_attr(attr_pot), C_NULL, C_NULL
        )

        # ✅ 修正：qn_pot 本身就是 Ptr，直接傳入即可
        UA_Variant_clear(Base.unsafe_convert(Ptr{UA_Variant}, v_pot_init))
        UA_QualifiedName_clear(qn_pot)

        # --- 2. DCCurrent 節點 ---
        cur_str = "$(st_name).DCCurrent"
        cur_node_id = UA_NODEID_STRING_ALLOC(1, cur_str)
        attr_cur = Ref(UA_VariableAttributes_default[])
        set_cfield!(attr_cur, :displayName, UA_LOCALIZEDTEXT("en-US", "DCCurrent"))
        set_cfield!(attr_cur, :dataType, double_type_node_id)

        v_cur_init = Ref{UA_Variant}()
        UA_Variant_setScalarCopy(v_cur_init, Ref(base_cur), type_double_ptr)
        set_cfield!(attr_cur, :value, v_cur_init[])

        qn_cur = UA_QUALIFIEDNAME_ALLOC(1, cur_str)
        UA_Server_addVariableNode(
            server, cur_node_id, parent_node_id, parent_ref_type_id,
            qn_cur, variable_type_id,
            to_node_attr(attr_cur), C_NULL, C_NULL
        )

        # ✅ 修正：qn_cur 本身就是 Ptr，直接傳入即可
        UA_Variant_clear(Base.unsafe_convert(Ptr{UA_Variant}, v_cur_init))
        UA_QualifiedName_clear(qn_cur)

        push!(stations, Station(st_name, pot_node_id, cur_node_id, base_pot, base_cur))
    end

    println("100 個測站初始化完成！")
    println("--------------------------------------------------")
    println("  端點: opc.tcp://localhost:4840 開放全域連線 ")
    println("  每秒數據即時更新中...")
    println("--------------------------------------------------\n")

    # 🔑 關鍵新增：啟動 Server 的 TCP 網路監聽服務！
    status = UA_Server_run_startup(server)
    if status != 0
        println("❌ Server 網路服務啟動失敗，Status Code: ", status)
        return
    end

    # 主事件迴圈
    last_update = time()

    while true
        # 處理 OPC UA 連線與網路事件
        UA_Server_run_iterate(server, false)

        # 每 1 秒動態更新數據
        if time() - last_update >= 1.0
            last_update = time()
            for st in stations
                st.pot_val += (rand() - 0.5) * 0.010
                st.cur_val += (rand() - 0.5) * 1.0

                write_double_node(server, st.pot_node_id, st.pot_val)
                write_double_node(server, st.cur_node_id, st.cur_val)
            end
        end

        sleep(0.005) # 讓出 CPU 5ms，避免單核滿載
    end
end

# 🔑 PackageCompiler 的標準入口函式
function julia_main()::Cint
    try
        run_server()
    catch e
        Base.invokelatest(Base.display_error, e, catch_backtrace())
        return 1
    end
    return 0
end

end # module

