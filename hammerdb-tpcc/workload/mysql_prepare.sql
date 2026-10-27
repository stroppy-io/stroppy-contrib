-- HammerDB v6.1, GPL-3.0; see LICENSE and NOTICE.
-- Source commit: 2442b32756a484de4206bb0061d9bb28869beaf0
-- Source sha256: ee5aeef280dc5516894cd559d5d0ca29c89482df9c7994b8b461f8c5e5774de8

--+ prepare
--= slev_st
prepare slev_st from 'CALL SLEV(?,?,?,@stock_count)'

--= delivery_st
prepare delivery_st from 'CALL DELIVERY(?,?,?)'

--= ostat_st
prepare ostat_st from 'CALL OSTAT(?,?,?,?,?,@os_c_first,@os_c_middle,@os_c_balance,@os_o_id,@os_entdate,@os_o_carrier_id)'

--= payment_st
prepare payment_st from 'CALL PAYMENT(?,?,?,?,?,?,?,?,@p_w_street_1,@p_w_street_2,@p_w_city,@p_w_state,@p_w_zip,@p_d_street_1,@p_d_street_2,@p_d_city,@p_d_state,@p_d_zip,@p_c_first,@p_c_middle,@p_c_street_1,@p_c_street_2,@p_c_city,@p_c_state,@p_c_zip,@p_c_phone,@p_c_since,?,@p_c_credit_lim,@p_c_discount,?,@p_c_data,?)'

--= neword_st
prepare neword_st from 'CALL NEWORD(?,?,?,?,?,@disc,@last,@credit,@dtax,@wtax,?,?)'
